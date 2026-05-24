package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/BUAASubnet/UBAA.Server/internal/config"
	"github.com/BUAASubnet/UBAA.Server/internal/dto"
	"github.com/BUAASubnet/UBAA.Server/internal/storage"
	"github.com/BUAASubnet/UBAA.Server/internal/upstream"
	"github.com/coocood/freecache"
	"github.com/gofiber/fiber/v2"
)

func TestValidateBearerUsesLocalSessionWithoutUpstreamValidation(t *testing.T) {
	service := newTestAuthService(t)
	username := "2333"
	if err := service.commitUserSession(context.Background(), username, dto.UserData{Name: "Alice", SchoolID: username}); err != nil {
		t.Fatal(err)
	}
	token, err := service.generateJWT(username, time.Now(), time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	session, err := service.ValidateBearer(context.Background(), "Bearer "+token, true)
	if err != nil {
		t.Fatal(err)
	}
	if session == nil || session.Username != username {
		t.Fatalf("session = %#v", session)
	}
}

func TestLoginErrorMapsCaptchaRequiredLikeKtor(t *testing.T) {
	app := fiber.New()
	app.Get("/", func(c *fiber.Ctx) error {
		return loginError(c, CaptchaRequiredError{
			Captcha: CaptchaInfo{
				ID:          "captcha-1",
				Type:        "image",
				ImageURL:    "https://sso.example.test/captcha?captchaId=captcha-1",
				Base64Image: stringPtr("data:image/jpeg;base64,abc"),
			},
			Execution: "exec-1",
			Message:   "需要验证码",
		})
	})

	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/", nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusUnprocessableEntity {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var body CaptchaRequiredResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Captcha.ID != "captcha-1" || body.Execution != "exec-1" || body.Message != "需要验证码" {
		t.Fatalf("body = %#v", body)
	}
	if body.Captcha.Base64Image == nil || *body.Captcha.Base64Image != "data:image/jpeg;base64,abc" {
		t.Fatalf("base64 image = %#v", body.Captcha.Base64Image)
	}
}

func TestLoginErrorMapsTimeoutAndInvalidCredentials(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{name: "timeout", err: ErrUpstreamTimeout, status: fiber.StatusServiceUnavailable, code: "auth_upstream_timeout"},
		{name: "invalid", err: ErrInvalidToken, status: fiber.StatusUnauthorized, code: "invalid_credentials"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			app := fiber.New()
			app.Get("/", func(c *fiber.Ctx) error {
				return loginError(c, test.err)
			})
			resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/", nil))
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != test.status {
				t.Fatalf("status = %d", resp.StatusCode)
			}
			var body dto.ErrorResponse
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body.Error.Code != test.code {
				t.Fatalf("code = %q, want %q", body.Error.Code, test.code)
			}
		})
	}
}

func TestPerformCASLoginReturnsStructuredCaptchaRequiredError(t *testing.T) {
	service := newTestAuthService(t)
	var upstreamBase string
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<html><form id="fm1"><input name="execution" value="exec-42"></form><script>config.captcha = { type: 'image', id: 'cap-42' }</script></html>`))
		case "/captcha":
			if r.URL.Query().Get("captchaId") != "cap-42" {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write([]byte("jpeg-bytes"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstreamServer.Close()
	upstreamBase = upstreamServer.URL
	service.clients = upstream.NewClientFactory(service.db, testRewriter{base: upstreamBase})
	client, err := service.clients.NewNoRedirect("2333")
	if err != nil {
		t.Fatal(err)
	}

	err = service.performCASLogin(context.Background(), client, LoginRequest{Username: "2333", Password: "secret"}, "2333")
	var captchaErr CaptchaRequiredError
	if !errors.As(err, &captchaErr) {
		t.Fatalf("error = %v, want CaptchaRequiredError", err)
	}
	if captchaErr.Captcha.ID != "cap-42" || captchaErr.Execution != "exec-42" {
		t.Fatalf("captcha error = %#v", captchaErr)
	}
	if captchaErr.Captcha.Base64Image == nil || !strings.HasPrefix(*captchaErr.Captcha.Base64Image, "data:image/jpeg;base64,") {
		t.Fatalf("base64 image = %#v", captchaErr.Captcha.Base64Image)
	}
}

func TestLoginActivatesUCSessionBeforeValidation(t *testing.T) {
	service := newTestAuthService(t)
	var sawActivate bool
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login":
			if r.Method == http.MethodGet {
				_, _ = w.Write([]byte(`<html><form id="fm1"><input name="execution" value="exec-1"></form></html>`))
				return
			}
			http.Redirect(w, r, "/done", http.StatusFound)
		case "/done":
			_, _ = w.Write([]byte("ok"))
		case "/api/login":
			sawActivate = true
			_, _ = w.Write([]byte("ok"))
		case "/api/uc/status":
			if !sawActivate {
				t.Fatal("status checked before UC activation")
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"data":{"name":"Alice","schoolid":"2333"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstreamServer.Close()
	service.clients = upstream.NewClientFactory(service.db, testRewriter{base: upstreamServer.URL})

	response, err := service.Login(context.Background(), LoginRequest{Username: "2333", Password: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	if !sawActivate {
		t.Fatal("UC activation was not requested")
	}
	if response.User.SchoolID != "2333" || response.User.Name != "Alice" {
		t.Fatalf("user = %#v", response.User)
	}
}

func newTestAuthService(t *testing.T) *Service {
	t.Helper()
	cfg := config.Load()
	cfg.SQLitePath = t.TempDir() + "/test.db"
	db, err := storage.OpenSQLite(cfg.SQLitePath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	cache := freecache.NewCache(1024 * 1024)
	clients := upstream.NewClientFactory(db, upstream.NewURLRewriter())
	return NewService(cfg, db, cache, clients)
}

type testRewriter struct {
	base string
}

func (r testRewriter) UpstreamURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	return strings.TrimRight(r.base, "/") + parsed.RequestURI()
}
