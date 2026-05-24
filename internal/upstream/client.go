package upstream

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/BUAASubnet/UBAA.Server/internal/storage"
	"golang.org/x/net/publicsuffix"
)

type ClientFactory struct {
	db       *storage.DB
	rewriter Rewriter
	mu       sync.Mutex
	clients  map[string]*SessionClient
}

type Rewriter interface {
	UpstreamURL(raw string) string
}

type SessionClient struct {
	Subject string
	Client  *http.Client
	jar     *persistentCookieJar
}

func NewClientFactory(db *storage.DB, rewriter Rewriter) *ClientFactory {
	return &ClientFactory{
		db:       db,
		rewriter: rewriter,
		clients:  map[string]*SessionClient{},
	}
}

func (f *ClientFactory) Get(subject string) (*SessionClient, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if client := f.clients[subject]; client != nil {
		return client, nil
	}
	jar, err := newPersistentCookieJar(f.db, subject)
	if err != nil {
		return nil, err
	}
	client := &SessionClient{
		Subject: subject,
		jar:     jar,
		Client: &http.Client{
			Jar:       jar,
			Timeout:   30 * time.Second,
			Transport: newTransport(),
		},
	}
	f.clients[subject] = client
	return client, nil
}

func (f *ClientFactory) NewNoRedirect(subject string) (*http.Client, error) {
	sessionClient, err := f.Get(subject)
	if err != nil {
		return nil, err
	}
	return &http.Client{
		Jar:       sessionClient.jar,
		Timeout:   30 * time.Second,
		Transport: newTransport(),
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}, nil
}

func newTransport() http.RoundTripper {
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return http.DefaultTransport
	}
	transport := base.Clone()
	if trustAllCerts() {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec
		return transport
	}
	transport.TLSClientConfig = &tls.Config{
		InsecureSkipVerify: true, //nolint:gosec
		VerifyConnection:   verifyBUAALibraryCertificate,
	}
	return transport
}

func trustAllCerts() bool {
	value := strings.TrimSpace(os.Getenv("TRUST_ALL_CERTS"))
	return strings.EqualFold(value, "true") || value == "1"
}

func verifyBUAALibraryCertificate(state tls.ConnectionState) error {
	if len(state.PeerCertificates) == 0 {
		return errors.New("tls peer certificate missing")
	}
	leaf := state.PeerCertificates[0]
	if err := leaf.VerifyHostname(state.ServerName); err != nil {
		return err
	}
	intermediates := x509.NewCertPool()
	for _, cert := range state.PeerCertificates[1:] {
		intermediates.AddCert(cert)
	}
	if _, err := leaf.Verify(x509.VerifyOptions{
		DNSName:       state.ServerName,
		Intermediates: intermediates,
	}); err == nil {
		return nil
	}
	if strings.EqualFold(state.ServerName, "booking.lib.buaa.edu.cn") {
		return nil
	}
	_, err := leaf.Verify(x509.VerifyOptions{DNSName: state.ServerName, Intermediates: intermediates})
	return err
}

func (f *ClientFactory) PromoteSubject(ctx context.Context, from, to string) error {
	if from == "" || to == "" || from == to {
		return nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	client := f.clients[from]
	if client != nil {
		delete(f.clients, from)
		client.Subject = to
		client.jar.subject = to
		f.clients[to] = client
	}
	return f.db.MigrateCookiesSubject(ctx, from, to)
}

func (f *ClientFactory) ClearSubject(ctx context.Context, subject string) error {
	f.mu.Lock()
	delete(f.clients, subject)
	f.mu.Unlock()
	return f.db.DeleteCookiesBySubject(ctx, subject)
}

func (f *ClientFactory) UpstreamURL(raw string) string {
	return f.rewriter.UpstreamURL(raw)
}

type persistentCookieJar struct {
	subject string
	db      *storage.DB
	jar     *cookiejar.Jar
	mu      sync.Mutex
}

func newPersistentCookieJar(db *storage.DB, subject string) (*persistentCookieJar, error) {
	base, err := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	if err != nil {
		return nil, err
	}
	jar := &persistentCookieJar{subject: subject, db: db, jar: base}
	records, err := db.LoadCookies(context.Background(), subject)
	if err != nil {
		return nil, err
	}
	grouped := map[string][]*http.Cookie{}
	for _, record := range records {
		if record.Expires != nil && record.Expires.Before(time.Now()) {
			_ = db.DeleteCookie(context.Background(), subject, record.Host, record.Path, record.Name)
			continue
		}
		scheme := "https"
		if !record.Secure {
			scheme = "http"
		}
		u := &url.URL{Scheme: scheme, Host: record.Host, Path: record.Path}
		cookie := &http.Cookie{
			Name:     record.Name,
			Value:    record.Value,
			Path:     record.Path,
			Domain:   record.Host,
			Secure:   record.Secure,
			HttpOnly: record.HTTPOnly,
			Raw:      record.Raw,
		}
		if record.Expires != nil {
			cookie.Expires = *record.Expires
		}
		grouped[u.String()] = append(grouped[u.String()], cookie)
	}
	for rawURL, cookies := range grouped {
		u, err := url.Parse(rawURL)
		if err == nil {
			base.SetCookies(u, cookies)
		}
	}
	return jar, nil
}

func (j *persistentCookieJar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.jar.SetCookies(u, cookies)
	now := time.Now()
	host := cookieHost(u)
	for _, cookie := range cookies {
		path := cookie.Path
		if path == "" {
			path = defaultCookiePath(u)
		}
		if cookie.MaxAge < 0 || (!cookie.Expires.IsZero() && cookie.Expires.Before(now)) {
			_ = j.db.DeleteCookie(context.Background(), j.subject, host, path, cookie.Name)
			continue
		}
		var expires *time.Time
		if !cookie.Expires.IsZero() {
			expires = &cookie.Expires
		}
		_ = j.db.SaveCookie(context.Background(), storage.CookieRecord{
			Subject:  j.subject,
			Host:     host,
			Path:     path,
			Name:     cookie.Name,
			Value:    cookie.Value,
			Raw:      cookie.Raw,
			Expires:  expires,
			Secure:   cookie.Secure,
			HTTPOnly: cookie.HttpOnly,
			SameSite: sameSiteString(cookie.SameSite),
			Updated:  now,
		})
	}
}

func (j *persistentCookieJar) Cookies(u *url.URL) []*http.Cookie {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.jar.Cookies(u)
}

func cookieHost(u *url.URL) string {
	host := u.Hostname()
	if host == "" {
		return u.Host
	}
	return host
}

func defaultCookiePath(u *url.URL) string {
	path := u.EscapedPath()
	if path == "" || !strings.HasPrefix(path, "/") {
		return "/"
	}
	if strings.Count(path, "/") <= 1 {
		return "/"
	}
	index := strings.LastIndex(path, "/")
	if index <= 0 {
		return "/"
	}
	return path[:index]
}

func sameSiteString(value http.SameSite) string {
	switch value {
	case http.SameSiteDefaultMode:
		return "Default"
	case http.SameSiteLaxMode:
		return "Lax"
	case http.SameSiteStrictMode:
		return "Strict"
	case http.SameSiteNoneMode:
		return "None"
	default:
		return ""
	}
}
