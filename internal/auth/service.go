package auth

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/BUAASubnet/UBAA.Server/internal/config"
	"github.com/BUAASubnet/UBAA.Server/internal/dto"
	"github.com/BUAASubnet/UBAA.Server/internal/httpx"
	"github.com/BUAASubnet/UBAA.Server/internal/storage"
	"github.com/BUAASubnet/UBAA.Server/internal/upstream"
	"github.com/PuerkitoBio/goquery"
	"github.com/coocood/freecache"
	"github.com/golang-jwt/jwt/v5"
)

const (
	jwtIssuer   = "ubaa-server"
	jwtAudience = "ubaa-users"
)

var ErrInvalidToken = errors.New("invalid token")
var ErrCaptchaRequired = errors.New("captcha required")
var ErrUpstreamTimeout = errors.New("auth upstream timeout")

type Service struct {
	cfg        config.Config
	db         *storage.DB
	cache      *freecache.Cache
	clients    *upstream.ClientFactory
	loginSlots chan struct{}
}

type Session struct {
	Username        string       `json:"username"`
	UserData        dto.UserData `json:"userData"`
	AuthenticatedAt time.Time    `json:"authenticatedAt"`
	LastActivity    time.Time    `json:"lastActivity"`
	PortalType      string       `json:"portalType"`
}

func NewService(cfg config.Config, db *storage.DB, cache *freecache.Cache, clients *upstream.ClientFactory) *Service {
	maxConcurrency := cfg.LoginMaxConcurrency
	if maxConcurrency < 1 {
		maxConcurrency = 1
	}
	return &Service{
		cfg:        cfg,
		db:         db,
		cache:      cache,
		clients:    clients,
		loginSlots: make(chan struct{}, maxConcurrency),
	}
}

func (s *Service) Preload(ctx context.Context, clientID string) (LoginPreloadResponse, error) {
	now := time.Now()
	_, err := s.db.ExecContext(ctx, `INSERT INTO prelogin_sessions (client_id, touched_at)
		VALUES (?, ?)
		ON CONFLICT(client_id) DO UPDATE SET touched_at = excluded.touched_at`,
		clientID, now.Format(time.RFC3339Nano))
	if err != nil {
		return LoginPreloadResponse{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, s.cfg.AuthPreloadTimeout)
	defer cancel()
	subject := preloginSubject(clientID)
	noRedirect, err := s.clients.NewNoRedirect(subject)
	if err != nil {
		return LoginPreloadResponse{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.clients.UpstreamURL(ssoLoginURL), nil)
	if err != nil {
		return LoginPreloadResponse{}, err
	}
	resp, err := noRedirect.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return LoginPreloadResponse{}, ErrUpstreamTimeout
		}
		return LoginPreloadResponse{
			CaptchaRequired: false,
			ClientID:        &clientID,
		}, nil
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 && resp.StatusCode <= 399 {
		if err := s.activateUCSession(ctx, subject); err != nil {
			return LoginPreloadResponse{
				CaptchaRequired: false,
				ClientID:        &clientID,
			}, nil
		}
		user, err := s.validateUCSession(ctx, subject)
		if err != nil && errors.Is(err, ErrUpstreamTimeout) && ctx.Err() != nil {
			return LoginPreloadResponse{}, ErrUpstreamTimeout
		}
		if err == nil {
			if err := s.clients.PromoteSubject(ctx, subject, user.SchoolID); err != nil {
				return LoginPreloadResponse{}, err
			}
			if err := s.commitUserSession(ctx, user.SchoolID, user); err != nil {
				return LoginPreloadResponse{}, err
			}
			tokens, err := s.issueTokens(ctx, user.SchoolID)
			if err != nil {
				return LoginPreloadResponse{}, err
			}
			return LoginPreloadResponse{
				CaptchaRequired:       false,
				ClientID:              &clientID,
				AccessToken:           &tokens.AccessToken,
				RefreshToken:          &tokens.RefreshToken,
				AccessTokenExpiresAt:  &tokens.AccessTokenExpiresAt,
				RefreshTokenExpiresAt: &tokens.RefreshTokenExpiresAt,
				UserData:              &user,
			}, nil
		}
	}
	if resp.StatusCode != http.StatusOK {
		return LoginPreloadResponse{
			CaptchaRequired: false,
			ClientID:        &clientID,
		}, nil
	}
	body, _ := io.ReadAll(resp.Body)
	html := string(body)
	execution := extractExecution(html)
	captcha := detectCaptcha(html, s.clients.UpstreamURL(ssoCaptchaURL))
	if captcha != nil {
		if image := s.fetchCaptcha(ctx, subject, captcha.ID); len(image) > 0 {
			encoded := "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(image)
			captcha.Base64Image = &encoded
		}
		return LoginPreloadResponse{
			CaptchaRequired: true,
			Captcha:         captcha,
			Execution:       stringPtr(execution),
			ClientID:        &clientID,
		}, nil
	}
	return LoginPreloadResponse{
		CaptchaRequired: false,
		Execution:       stringPtr(execution),
		ClientID:        &clientID,
	}, nil
}

func (s *Service) Login(ctx context.Context, request LoginRequest) (LoginResponse, error) {
	username := strings.TrimSpace(request.Username)
	password := strings.TrimSpace(request.Password)
	if username == "" || password == "" {
		return LoginResponse{}, ErrInvalidToken
	}
	ctx, cancel := context.WithTimeout(ctx, s.cfg.AuthLoginTimeout)
	defer cancel()
	release, err := s.acquireLoginSlot(ctx)
	if err != nil {
		return LoginResponse{}, err
	}
	defer release()
	subject := username
	if request.ClientID != nil && strings.TrimSpace(*request.ClientID) != "" {
		subject = preloginSubject(strings.TrimSpace(*request.ClientID))
	}
	noRedirect, err := s.clients.NewNoRedirect(subject)
	if err != nil {
		return LoginResponse{}, err
	}
	if err := s.performCASLogin(ctx, noRedirect, request, subject); err != nil {
		return LoginResponse{}, err
	}
	if err := s.activateUCSession(ctx, subject); err != nil {
		return LoginResponse{}, err
	}
	user, validationErr := s.validateUCSession(ctx, subject)
	if errors.Is(validationErr, ErrUpstreamTimeout) {
		return LoginResponse{}, validationErr
	}
	ok := validationErr == nil
	if !ok {
		return LoginResponse{}, ErrInvalidToken
	}
	committedUsername := user.SchoolID
	if committedUsername == "" {
		committedUsername = username
		user.SchoolID = committedUsername
	}
	if subject != committedUsername {
		if err := s.clients.PromoteSubject(ctx, subject, committedUsername); err != nil {
			return LoginResponse{}, err
		}
	}
	if err := s.commitUserSession(ctx, committedUsername, user); err != nil {
		return LoginResponse{}, err
	}
	tokens, err := s.issueTokens(ctx, committedUsername)
	if err != nil {
		return LoginResponse{}, err
	}
	return LoginResponse{
		User:                  user,
		AccessToken:           tokens.AccessToken,
		RefreshToken:          tokens.RefreshToken,
		AccessTokenExpiresAt:  tokens.AccessTokenExpiresAt,
		RefreshTokenExpiresAt: tokens.RefreshTokenExpiresAt,
	}, nil
}

func (s *Service) Refresh(ctx context.Context, refreshToken string) (*TokenRefreshResponse, error) {
	hash := hashToken(refreshToken)
	record, err := s.db.FindRefreshToken(ctx, hash)
	if err != nil {
		return nil, err
	}
	if record == nil || !record.ExpiresAt.After(time.Now()) {
		return nil, nil
	}
	session, err := s.SessionByUsername(ctx, record.Username, true)
	if err != nil || session == nil {
		_ = s.db.DeleteRefreshTokenByUsername(ctx, record.Username)
		return nil, err
	}
	tokens, err := s.issueTokens(ctx, record.Username)
	if err != nil {
		return nil, err
	}
	return &tokens, nil
}

func (s *Service) Logout(ctx context.Context, username string) error {
	_ = s.cache.Del([]byte(cacheSessionKey(username)))
	_ = s.clients.ClearSubject(ctx, username)
	if err := s.db.DeleteRefreshTokenByUsername(ctx, username); err != nil {
		return err
	}
	return s.db.DeleteSession(ctx, username)
}

func (s *Service) TokenUsername(tokenString string) (string, error) {
	claims := jwt.MapClaims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (any, error) {
		return []byte(s.cfg.JWTSecret), nil
	}, jwt.WithAudience(jwtAudience), jwt.WithIssuer(jwtIssuer))
	if err != nil || !token.Valid {
		return "", ErrInvalidToken
	}
	subject, err := claims.GetSubject()
	if err != nil || subject == "" {
		return "", ErrInvalidToken
	}
	return subject, nil
}

func (s *Service) SessionByUsername(ctx context.Context, username string, touch bool) (*Session, error) {
	if session, ok := s.cachedSession(username); ok {
		if touch {
			session.LastActivity = time.Now()
			_ = s.cacheSession(username, session)
			_ = s.db.TouchSession(ctx, username, session.LastActivity)
		}
		return &session, nil
	}
	record, err := s.db.LoadSession(ctx, username)
	if err != nil || record == nil {
		return nil, err
	}
	if record.LastActivity.Add(s.cfg.SessionTTL).Before(time.Now()) {
		_ = s.db.DeleteSession(ctx, username)
		return nil, nil
	}
	session := Session{
		Username:        record.Username,
		UserData:        record.UserData,
		AuthenticatedAt: record.AuthenticatedAt,
		LastActivity:    record.LastActivity,
		PortalType:      record.PortalType,
	}
	if touch {
		session.LastActivity = time.Now()
		_ = s.db.TouchSession(ctx, username, session.LastActivity)
	}
	_ = s.cacheSession(username, session)
	return &session, nil
}

func (s *Service) ValidateBearer(ctx context.Context, authorization string, touch bool) (*Session, error) {
	if !strings.HasPrefix(authorization, "Bearer ") {
		return nil, ErrInvalidToken
	}
	tokenString := strings.TrimSpace(strings.TrimPrefix(authorization, "Bearer "))
	username, err := s.TokenUsername(tokenString)
	if err != nil {
		return nil, err
	}
	session, err := s.SessionByUsername(ctx, username, touch)
	if err != nil {
		return nil, err
	}
	if session == nil {
		return nil, ErrInvalidToken
	}
	return session, nil
}

func (s *Service) ValidateBearerUpstream(ctx context.Context, authorization string) (*Session, error) {
	session, err := s.ValidateBearer(ctx, authorization, true)
	if err != nil || session == nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, s.cfg.AuthValidationTimeout)
	defer cancel()
	user, err := s.validateUCSession(ctx, session.Username)
	if err != nil {
		if errors.Is(err, ErrUpstreamTimeout) {
			return nil, err
		}
		_ = s.db.DeleteSession(ctx, session.Username)
		_ = s.cache.Del([]byte(cacheSessionKey(session.Username)))
		return nil, ErrInvalidToken
	}
	session.UserData = user
	session.LastActivity = time.Now()
	_ = s.cacheSession(session.Username, *session)
	_ = s.db.TouchSession(ctx, session.Username, session.LastActivity)
	return session, nil
}

func (s *Service) acquireLoginSlot(ctx context.Context) (func(), error) {
	select {
	case s.loginSlots <- struct{}{}:
		return func() { <-s.loginSlots }, nil
	case <-ctx.Done():
		return nil, ErrUpstreamTimeout
	}
}

func (s *Service) RecordLoginStat(ctx context.Context, request LoginStatsReportRequest) error {
	return s.db.SaveLoginStat(ctx, request.Username, request.SuccessMode, request.ConnectionMode, time.Now())
}

func (s *Service) CleanupExpired(ctx context.Context) error {
	_, err := s.db.DeleteExpiredSessions(ctx, time.Now().Add(-s.cfg.SessionTTL))
	if err != nil {
		return err
	}
	_, err = s.db.DeleteExpiredPreLoginSessions(ctx, time.Now().Add(-s.cfg.PreLoginTTL))
	if err != nil {
		return err
	}
	_, err = s.db.DeleteExpiredRefreshTokens(ctx, time.Now())
	return err
}

func (s *Service) ActiveSessionCount(ctx context.Context) int {
	count, err := s.db.CountActiveSessions(ctx, time.Now().Add(-s.cfg.SessionTTL))
	if err != nil {
		return 0
	}
	return count
}

func (s *Service) ActivePreLoginSessionCount(ctx context.Context) int {
	count, err := s.db.CountActivePreLoginSessions(ctx, time.Now().Add(-s.cfg.PreLoginTTL))
	if err != nil {
		return 0
	}
	return count
}

func (s *Service) issueTokens(ctx context.Context, username string) (TokenRefreshResponse, error) {
	now := time.Now()
	accessExpiresAt := now.Add(s.cfg.AccessTokenTTL)
	refreshExpiresAt := now.Add(s.cfg.RefreshTokenTTL)
	accessToken, err := s.generateJWT(username, now, accessExpiresAt)
	if err != nil {
		return TokenRefreshResponse{}, err
	}
	refreshToken, err := randomToken()
	if err != nil {
		return TokenRefreshResponse{}, err
	}
	if err := s.db.SaveRefreshToken(ctx, storage.RefreshTokenRecord{
		Username:  username,
		TokenHash: hashToken(refreshToken),
		IssuedAt:  now,
		ExpiresAt: refreshExpiresAt,
	}); err != nil {
		return TokenRefreshResponse{}, err
	}
	return TokenRefreshResponse{
		AccessToken:           accessToken,
		RefreshToken:          refreshToken,
		AccessTokenExpiresAt:  accessExpiresAt.Format(time.RFC3339Nano),
		RefreshTokenExpiresAt: refreshExpiresAt.Format(time.RFC3339Nano),
	}, nil
}

func (s *Service) generateJWT(username string, issuedAt, expiresAt time.Time) (string, error) {
	claims := jwt.MapClaims{
		"iss":      jwtIssuer,
		"aud":      jwtAudience,
		"sub":      username,
		"iat":      issuedAt.Unix(),
		"exp":      expiresAt.Unix(),
		"username": username,
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(s.cfg.JWTSecret))
}

func (s *Service) cacheSession(username string, session Session) error {
	raw, err := json.Marshal(session)
	if err != nil {
		return err
	}
	ttl := int(s.cfg.SessionTTL.Seconds())
	if ttl < 1 {
		ttl = 1
	}
	return s.cache.Set([]byte(cacheSessionKey(username)), raw, ttl)
}

func (s *Service) cachedSession(username string) (Session, bool) {
	raw, err := s.cache.Get([]byte(cacheSessionKey(username)))
	if err != nil {
		return Session{}, false
	}
	var session Session
	if err := json.Unmarshal(raw, &session); err != nil {
		return Session{}, false
	}
	if session.LastActivity.Add(s.cfg.SessionTTL).Before(time.Now()) {
		_ = s.cache.Del([]byte(cacheSessionKey(username)))
		return Session{}, false
	}
	return session, true
}

func cacheSessionKey(username string) string {
	return "session:" + username
}

func randomToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (s *Service) commitUserSession(ctx context.Context, username string, user dto.UserData) error {
	now := time.Now()
	record := storage.SessionRecord{
		Username:        username,
		UserData:        user,
		AuthenticatedAt: now,
		LastActivity:    now,
		PortalType:      "UNKNOWN",
	}
	if err := s.db.SaveSession(ctx, record); err != nil {
		return err
	}
	return s.cacheSession(username, Session{
		Username:        username,
		UserData:        user,
		AuthenticatedAt: now,
		LastActivity:    now,
		PortalType:      "UNKNOWN",
	})
}

func (s *Service) performCASLogin(ctx context.Context, client *http.Client, request LoginRequest, subject string) error {
	loginURL := s.clients.UpstreamURL(ssoLoginURL)
	html := ""
	execution := strings.TrimSpace(valuePtr(request.Execution))
	if execution == "" {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, loginURL, nil)
		if err != nil {
			return err
		}
		req.Header.Set("User-Agent", defaultUserAgent)
		resp, err := client.Do(req)
		if err != nil {
			return ErrUpstreamTimeout
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		html = string(body)
		if resp.StatusCode != http.StatusOK && (resp.StatusCode < 300 || resp.StatusCode > 399) {
			return ErrInvalidToken
		}
		if tip := extractTipText(html); tip != "" {
			return ErrInvalidToken
		}
		execution = extractExecution(html)
		if captcha := detectCaptcha(html, s.clients.UpstreamURL(ssoCaptchaURL)); captcha != nil && strings.TrimSpace(valuePtr(request.Captcha)) == "" {
			if image := s.fetchCaptchaWithClient(ctx, client, captcha.ID); len(image) > 0 {
				encoded := "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(image)
				captcha.Base64Image = &encoded
			}
			return CaptchaRequiredError{
				Captcha:   *captcha,
				Execution: execution,
				Message:   "需要验证码",
			}
		}
	}
	form := buildCASLoginForm(html, request, execution)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, loginURL, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", defaultUserAgent)
	resp, err := client.Do(req)
	if err != nil {
		return ErrUpstreamTimeout
	}
	defer resp.Body.Close()
	return s.followRedirectsAndCheckError(ctx, client, resp)
}

func (s *Service) followRedirectsAndCheckError(ctx context.Context, client *http.Client, response *http.Response) error {
	current := response
	passwordExpiryIgnored := false
	for {
		for current.StatusCode >= 300 && current.StatusCode <= 399 {
			location := current.Header.Get("Location")
			if location == "" {
				break
			}
			nextURL := resolveLocation(current.Request.URL, location)
			_ = current.Body.Close()
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, nextURL, nil)
			if err != nil {
				return err
			}
			req.Header.Set("User-Agent", defaultUserAgent)
			current, err = client.Do(req)
			if err != nil {
				return ErrUpstreamTimeout
			}
		}
		bodyBytes, _ := io.ReadAll(current.Body)
		body := string(bodyBytes)
		current.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		if isIgnorablePasswordExpiryPage(body) {
			if passwordExpiryIgnored {
				return ErrInvalidToken
			}
			execution := extractExecution(body)
			if execution == "" {
				return ErrInvalidToken
			}
			passwordExpiryIgnored = true
			form := url.Values{}
			form.Set("execution", execution)
			form.Set("_eventId", "ignoreAndContinue")
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, stripQuery(current.Request.URL.String()), strings.NewReader(form.Encode()))
			if err != nil {
				return err
			}
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			current, err = client.Do(req)
			if err != nil {
				return ErrUpstreamTimeout
			}
			continue
		}
		if strings.Contains(current.Request.URL.String(), "exception.message=") {
			return ErrInvalidToken
		}
		if current.StatusCode == http.StatusUnauthorized || findLoginError(body) != "" || strings.Contains(body, `input name="execution"`) {
			return ErrInvalidToken
		}
		return nil
	}
}

func (s *Service) validateUCSession(ctx context.Context, subject string) (dto.UserData, error) {
	client, err := s.clients.Get(subject)
	if err != nil {
		return dto.UserData{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.clients.UpstreamURL(ucStatusURL), nil)
	if err != nil {
		return dto.UserData{}, err
	}
	req.Header.Set("Accept", "application/json, text/javascript, */*; q=0.01")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("User-Agent", defaultUserAgent)
	resp, err := client.Client.Do(req)
	if err != nil {
		return dto.UserData{}, ErrUpstreamTimeout
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return dto.UserData{}, ErrInvalidToken
	}
	var parsed struct {
		Code int           `json:"code"`
		Data *dto.UserInfo `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil || parsed.Code != 0 || parsed.Data == nil {
		return dto.UserData{}, ErrInvalidToken
	}
	return dto.UserData{
		Name:     stringValue(parsed.Data.Name),
		SchoolID: stringValue(parsed.Data.SchoolID),
	}, nil
}

func (s *Service) activateUCSession(ctx context.Context, subject string) error {
	client, err := s.clients.Get(subject)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.clients.UpstreamURL(ucLoginURL), nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", defaultUserAgent)
	resp, err := client.Client.Do(req)
	if err != nil {
		return ErrUpstreamTimeout
	}
	defer resp.Body.Close()
	return nil
}

func (s *Service) fetchCaptcha(ctx context.Context, subject, captchaID string) []byte {
	client, err := s.clients.Get(subject)
	if err != nil {
		return nil
	}
	return s.fetchCaptchaWithClient(ctx, client.Client, captchaID)
}

func (s *Service) fetchCaptchaWithClient(ctx context.Context, client *http.Client, captchaID string) []byte {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.clients.UpstreamURL(ssoCaptchaURL)+"?captchaId="+url.QueryEscape(captchaID), nil)
	if err != nil {
		return nil
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	body, _ := io.ReadAll(resp.Body)
	return body
}

func (s *Service) CaptchaImage(ctx context.Context, captchaID string) ([]byte, error) {
	if strings.TrimSpace(captchaID) == "" {
		return nil, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.clients.UpstreamURL(ssoCaptchaURL)+"?captchaId="+url.QueryEscape(captchaID), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", defaultUserAgent)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, nil
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return body, nil
}

func buildCASLoginForm(html string, request LoginRequest, execution string) url.Values {
	form := url.Values{}
	if strings.TrimSpace(html) != "" {
		doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
		if err == nil {
			doc.Find("form#fm1 input[name], form[action] input[name]").Each(func(_ int, input *goquery.Selection) {
				name, _ := input.Attr("name")
				name = strings.TrimSpace(name)
				if name == "" || name == "username" || name == "password" {
					return
				}
				inputType := strings.ToLower(strings.TrimSpace(attr(input, "type")))
				if inputType == "submit" || inputType == "button" || inputType == "image" {
					return
				}
				if inputType == "checkbox" {
					if _, ok := input.Attr("checked"); ok {
						form.Add(name, defaultString(attr(input, "value"), "on"))
					}
					return
				}
				if value := attr(input, "value"); value != "" || inputType == "hidden" {
					form.Add(name, value)
				}
			})
		}
	}
	form.Set("username", request.Username)
	form.Set("password", request.Password)
	form.Set("submit", "登录")
	form.Set("_eventId", "submit")
	if execution != "" && form.Get("execution") == "" {
		form.Set("execution", execution)
	}
	if form.Get("type") == "" {
		form.Set("type", "username_password")
	}
	if captcha := strings.TrimSpace(valuePtr(request.Captcha)); captcha != "" {
		form.Set("captcha", captcha)
		form.Set("captchaResponse", captcha)
	}
	return form
}

func extractExecution(html string) string {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return ""
	}
	value, _ := doc.Find("input[name=execution]").First().Attr("value")
	return strings.TrimSpace(value)
}

func detectCaptcha(html, captchaURLBase string) *CaptchaInfo {
	re := regexp.MustCompile(`config\.captcha\s*=\s*\{\s*type:\s*['"]([^'"]+)['"],\s*id:\s*['"]([^'"]+)['"]`)
	match := re.FindStringSubmatch(html)
	if len(match) < 3 {
		return nil
	}
	return &CaptchaInfo{
		ID:       match[2],
		Type:     match[1],
		ImageURL: captchaURLBase + "?captchaId=" + url.QueryEscape(match[2]),
	}
}

func findLoginError(html string) string {
	if tip := extractTipText(html); tip != "" {
		return tip
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return ""
	}
	selectors := []string{
		"div.alert.alert-danger#errorDiv p",
		"div.alert.alert-danger#errorDiv",
		"div.errors",
		"p.errors",
		"span.errors",
		".tip-text",
	}
	for _, selector := range selectors {
		if text := strings.TrimSpace(doc.Find(selector).Text()); text != "" {
			return text
		}
	}
	return ""
}

func extractTipText(html string) string {
	re := regexp.MustCompile(`(?i)<div class=\"tip-text\">([^<]+)</div>`)
	match := re.FindStringSubmatch(html)
	if len(match) < 2 {
		return ""
	}
	return strings.TrimSpace(match[1])
}

func isIgnorablePasswordExpiryPage(html string) bool {
	if strings.TrimSpace(html) == "" || extractExecution(html) == "" {
		return false
	}
	return strings.Contains(strings.ToLower(html), "continueform") ||
		strings.Contains(strings.ToLower(html), "ignoreandcontinue") ||
		strings.Contains(html, "账号存在安全风险") ||
		strings.Contains(html, "密码过期")
}

func resolveLocation(base *url.URL, location string) string {
	parsed, err := url.Parse(location)
	if err != nil {
		return location
	}
	return base.ResolveReference(parsed).String()
}

func stripQuery(raw string) string {
	return strings.SplitN(raw, "?", 2)[0]
}

func attr(selection *goquery.Selection, name string) string {
	value, _ := selection.Attr(name)
	return value
}

func preloginSubject(clientID string) string {
	return "prelogin:" + clientID
}

func stringPtr(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return &value
}

func valuePtr(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

const (
	ssoLoginURL      = "https://sso.buaa.edu.cn/login"
	ssoCaptchaURL    = "https://sso.buaa.edu.cn/captcha"
	ucLoginURL       = "https://uc.buaa.edu.cn/api/login?target=https%3A%2F%2Fuc.buaa.edu.cn%2F%23%2Fuser%2Flogin"
	ucStatusURL      = "https://uc.buaa.edu.cn/api/uc/status"
	defaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125 Safari/537.36"
)

var _ = httpx.UserFacingMessage
