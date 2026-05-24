package auth

import "github.com/BUAASubnet/UBAA.Server/internal/dto"

type LoginRequest struct {
	Username  string  `json:"username"`
	Password  string  `json:"password"`
	Captcha   *string `json:"captcha"`
	Execution *string `json:"execution"`
	ClientID  *string `json:"clientId"`
}

type LoginResponse struct {
	User                  dto.UserData `json:"user"`
	AccessToken           string       `json:"accessToken"`
	RefreshToken          string       `json:"refreshToken"`
	AccessTokenExpiresAt  string       `json:"accessTokenExpiresAt"`
	RefreshTokenExpiresAt string       `json:"refreshTokenExpiresAt"`
}

type CaptchaInfo struct {
	ID          string  `json:"id"`
	Type        string  `json:"type"`
	ImageURL    string  `json:"imageUrl"`
	Base64Image *string `json:"base64Image"`
}

type CaptchaRequiredResponse struct {
	Captcha   CaptchaInfo `json:"captcha"`
	Execution string      `json:"execution"`
	Message   string      `json:"message"`
}

type CaptchaRequiredError struct {
	Captcha   CaptchaInfo
	Execution string
	Message   string
}

func (e CaptchaRequiredError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return "captcha required"
}

func (e CaptchaRequiredError) Unwrap() error {
	return ErrCaptchaRequired
}

type LoginPreloadRequest struct {
	ClientID string `json:"clientId"`
}

type LoginPreloadResponse struct {
	CaptchaRequired       bool          `json:"captchaRequired"`
	Captcha               *CaptchaInfo  `json:"captcha"`
	Execution             *string       `json:"execution"`
	ClientID              *string       `json:"clientId"`
	AccessToken           *string       `json:"accessToken"`
	RefreshToken          *string       `json:"refreshToken"`
	AccessTokenExpiresAt  *string       `json:"accessTokenExpiresAt"`
	RefreshTokenExpiresAt *string       `json:"refreshTokenExpiresAt"`
	UserData              *dto.UserData `json:"userData"`
}

type LoginStatsReportRequest struct {
	Username       string `json:"username"`
	SuccessMode    string `json:"successMode"`
	ConnectionMode string `json:"connectionMode"`
}

type TokenRefreshRequest struct {
	RefreshToken string `json:"refreshToken"`
}

type TokenRefreshResponse struct {
	AccessToken           string `json:"accessToken"`
	RefreshToken          string `json:"refreshToken"`
	AccessTokenExpiresAt  string `json:"accessTokenExpiresAt"`
	RefreshTokenExpiresAt string `json:"refreshTokenExpiresAt"`
}

type SessionStatusResponse struct {
	User            dto.UserData `json:"user"`
	LastActivity    string       `json:"lastActivity"`
	AuthenticatedAt string       `json:"authenticatedAt"`
}
