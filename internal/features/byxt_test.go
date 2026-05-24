package features

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BUAASubnet/UBAA.Server/internal/storage"
	"github.com/BUAASubnet/UBAA.Server/internal/upstream"
)

func TestEnsureUndergradPortalAcceptsReadyCurrentUser(t *testing.T) {
	service, server := newFeatureTestService(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/jwapp/sys/homeapp/api/home/currentUser.do" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":"0","datas":{}}`))
			return
		}
		http.NotFound(w, r)
	})
	defer server.Close()

	if err := service.ensureUndergradPortal(context.Background(), "2333"); err != nil {
		t.Fatalf("ensureUndergradPortal() error = %v", err)
	}
}

func TestEnsureUndergradPortalProbesWithoutAjaxHeader(t *testing.T) {
	service, server := newFeatureTestService(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/jwapp/sys/homeapp/api/home/currentUser.do" {
			if r.Header.Get("X-Requested-With") != "" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`<html><title>401</title></html>`))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":"0","datas":{}}`))
			return
		}
		http.NotFound(w, r)
	})
	defer server.Close()

	if err := service.ensureUndergradPortal(context.Background(), "2333"); err != nil {
		t.Fatalf("ensureUndergradPortal() error = %v", err)
	}
}

func TestEnsureUndergradPortalReturnsUnauthenticatedForSSOPage(t *testing.T) {
	service, server := newFeatureTestService(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/jwapp/sys/homeapp/api/home/currentUser.do" {
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(`<html><input name="execution" value="e1">统一身份认证</html>`))
			return
		}
		http.NotFound(w, r)
	})
	defer server.Close()

	if err := service.ensureUndergradPortal(context.Background(), "2333"); !errors.Is(err, ErrFeatureUnauthenticated) {
		t.Fatalf("ensureUndergradPortal() error = %v, want ErrFeatureUnauthenticated", err)
	}
}

func TestEnsureUndergradPortalDetectsGraduateAccount(t *testing.T) {
	service, server := newFeatureTestService(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/jwapp/sys/homeapp/api/home/currentUser.do":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(`<html>not the undergrad portal</html>`))
		case "/gsapp/sys/yjsemaphome/modules/pubWork/getUserInfo.do":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":"0","data":{"userId":"g1"}}`))
		default:
			http.NotFound(w, r)
		}
	})
	defer server.Close()

	if err := service.ensureUndergradPortal(context.Background(), "2333"); !errors.Is(err, ErrUnsupportedPortal) {
		t.Fatalf("ensureUndergradPortal() error = %v, want ErrUnsupportedPortal", err)
	}
}

func TestLibBookRequestJSONMapsHTMLLoginPageToAuthFailure(t *testing.T) {
	service, server := newFeatureTestService(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><input name="execution" value="e1">统一身份认证</html>`))
	})
	defer server.Close()
	client := &libBookClient{username: "2333", service: service, token: "token"}

	_, err := client.requestJSON(context.Background(), "list_libraries", "space/pcTopFor", map[string]any{"day": "2026-05-24"}, true, false)
	if !errors.Is(err, ErrLibBookAuth) {
		t.Fatalf("requestJSON() error = %v, want ErrLibBookAuth", err)
	}
}

func newFeatureTestService(t *testing.T, handler http.HandlerFunc) (*Service, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	db, err := storage.OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		server.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	clients := upstream.NewClientFactory(db, featureTestRewriter{base: server.URL})
	return NewService(clients), server
}

type featureTestRewriter struct {
	base string
}

func (r featureTestRewriter) UpstreamURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	return strings.TrimRight(r.base, "/") + parsed.RequestURI()
}
