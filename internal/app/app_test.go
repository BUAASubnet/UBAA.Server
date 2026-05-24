package app

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BUAASubnet/UBAA.Server/internal/auth"
	"github.com/BUAASubnet/UBAA.Server/internal/config"
	"github.com/BUAASubnet/UBAA.Server/internal/features"
	"github.com/BUAASubnet/UBAA.Server/internal/storage"
	"github.com/BUAASubnet/UBAA.Server/internal/upstream"
	"github.com/coocood/freecache"
	"github.com/gofiber/fiber/v2"
)

func TestRootAndAnonymousEndpoints(t *testing.T) {
	server := newTestApp(t)
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Request-ID", "req-test-123")
	resp, err := server.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("root status = %d", resp.StatusCode)
	}
	if got := resp.Header.Get("X-Request-ID"); got != "req-test-123" {
		t.Fatalf("request id header = %q", got)
	}

	resp, err = server.Test(httptest.NewRequest("GET", "/api/v1/app/announcement", nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 204 {
		t.Fatalf("announcement status = %d", resp.StatusCode)
	}
}

func TestProtectedEndpointWithoutTokenReturnsKtorErrorShape(t *testing.T) {
	server := newTestApp(t)
	resp, err := server.Test(httptest.NewRequest("GET", "/api/v1/user/info", nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 401 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var body map[string]map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["error"]["code"] != "invalid_token" {
		t.Fatalf("error code = %q", body["error"]["code"])
	}
	if body["error"]["message"] != "登录状态已失效，请重新登录" {
		t.Fatalf("error message = %q", body["error"]["message"])
	}
}

func TestVersionRequiresClientVersion(t *testing.T) {
	server := newTestApp(t)
	resp, err := server.Test(httptest.NewRequest("GET", "/api/v1/app/version", nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 400 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestMetricsExposeCompatibilityGauges(t *testing.T) {
	server := newTestApp(t)
	resp, err := server.Test(httptest.NewRequest("GET", "/metrics", nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	bodyBytes := new(bytes.Buffer)
	if _, err := bodyBytes.ReadFrom(resp.Body); err != nil {
		t.Fatal(err)
	}
	body := bodyBytes.String()
	for _, metric := range []string{
		"ubaa_sessions_active",
		"ubaa_sessions_prelogin",
		"ubaa_signin_cache",
		"ubaa_bykc_cache",
		"ubaa_cgyy_cache",
		"ubaa_spoc_cache",
		"ubaa_judge_cache",
		"ubaa_libbook_cache",
		"ubaa_ygdk_cache",
		"ubaa_ygdk_context_cache",
		"ubaa_redis_ready",
		"ubaa_sqlite_ready",
	} {
		if !strings.Contains(body, metric+" ") {
			t.Fatalf("metrics body missing %s:\n%s", metric, body)
		}
	}
	if strings.Contains(body, "placeholder") {
		t.Fatalf("metrics still contains placeholder: %s", body)
	}
}

func TestLoginRejectsMalformedBody(t *testing.T) {
	server := newTestApp(t)
	req := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewBufferString("{"))
	req.Header.Set("Content-Type", "application/json")
	resp, err := server.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 400 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func newTestApp(t *testing.T) *fiber.App {
	t.Helper()
	tempDir := t.TempDir()
	cfg := config.Load()
	cfg.SQLitePath = filepath.Join(tempDir, "test.db")
	cfg.ServerVersion = "1.0.0"
	cfg.UpdateDownloadURL = "https://download.example.com"
	db, err := storage.OpenSQLite(cfg.SQLitePath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	cache := freecache.NewCache(1024 * 1024)
	clients := upstream.NewClientFactory(db, upstream.NewURLRewriter())
	authService := auth.NewService(cfg, db, cache, clients)
	return New(Dependencies{Config: cfg, DB: db, Cache: cache, Auth: authService, Features: features.NewService(clients)})
}
