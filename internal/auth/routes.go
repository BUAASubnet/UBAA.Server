package auth

import (
	"context"
	"errors"
	"strings"

	"github.com/BUAASubnet/UBAA.Server/internal/httpx"
	"github.com/gofiber/fiber/v2"
)

func RegisterRoutes(app fiber.Router, service *Service) {
	group := app.Group("/api/v1/auth")

	group.Post("/preload", func(c *fiber.Ctx) error {
		var request LoginPreloadRequest
		if err := c.BodyParser(&request); err != nil || strings.TrimSpace(request.ClientID) == "" {
			return httpx.Error(c, fiber.StatusBadRequest, "invalid_request", "请提供有效的客户端标识")
		}
		response, err := service.Preload(context.Background(), request.ClientID)
		if err != nil {
			return httpx.Error(c, fiber.StatusInternalServerError, "internal_server_error", "登录状态加载失败，请稍后重试")
		}
		return c.Status(fiber.StatusOK).JSON(response)
	})

	group.Post("/login", func(c *fiber.Ctx) error {
		var request LoginRequest
		if err := c.BodyParser(&request); err != nil {
			return httpx.Error(c, fiber.StatusBadRequest, "invalid_request", "登录请求格式不正确")
		}
		response, err := service.Login(context.Background(), request)
		if err != nil {
			return loginError(c, err)
		}
		return c.Status(fiber.StatusOK).JSON(response)
	})

	group.Post("/login-stats", func(c *fiber.Ctx) error {
		var request LoginStatsReportRequest
		if err := c.BodyParser(&request); err != nil {
			return httpx.Error(c, fiber.StatusBadRequest, "invalid_request", "登录统计请求格式不正确")
		}
		if strings.TrimSpace(request.Username) == "" {
			return httpx.Error(c, fiber.StatusBadRequest, "invalid_request", "请提供有效的用户名")
		}
		if err := service.RecordLoginStat(context.Background(), request); err != nil {
			return httpx.Error(c, fiber.StatusInternalServerError, "internal_server_error")
		}
		return c.SendStatus(fiber.StatusOK)
	})

	group.Post("/refresh", func(c *fiber.Ctx) error {
		var request TokenRefreshRequest
		if err := c.BodyParser(&request); err != nil {
			return httpx.Error(c, fiber.StatusBadRequest, "invalid_request", "刷新令牌请求格式不正确")
		}
		response, err := service.Refresh(context.Background(), request.RefreshToken)
		if err != nil {
			return httpx.Error(c, fiber.StatusInternalServerError, "internal_server_error")
		}
		if response == nil {
			return httpx.Error(c, fiber.StatusUnauthorized, "invalid_refresh_token")
		}
		return c.Status(fiber.StatusOK).JSON(response)
	})

	group.Get("/status", func(c *fiber.Ctx) error {
		session, err := service.ValidateBearerUpstream(context.Background(), c.Get(fiber.HeaderAuthorization))
		if errors.Is(err, ErrUpstreamTimeout) {
			return httpx.Error(c, fiber.StatusServiceUnavailable, "auth_upstream_timeout")
		}
		if err != nil || session == nil {
			return httpx.Error(c, fiber.StatusUnauthorized, "invalid_token")
		}
		return c.Status(fiber.StatusOK).JSON(SessionStatusResponse{
			User:            session.UserData,
			LastActivity:    session.LastActivity.String(),
			AuthenticatedAt: session.AuthenticatedAt.String(),
		})
	})

	group.Post("/logout", func(c *fiber.Ctx) error {
		session, err := service.ValidateBearer(context.Background(), c.Get(fiber.HeaderAuthorization), false)
		if err != nil || session == nil {
			return httpx.Error(c, fiber.StatusUnauthorized, "invalid_token")
		}
		if err := service.Logout(context.Background(), session.Username); err != nil {
			return httpx.Error(c, fiber.StatusInternalServerError, "internal_server_error")
		}
		return c.Status(fiber.StatusOK).JSON(fiber.Map{"message": "Logged out successfully"})
	})

	group.Get("/captcha/:captchaId", func(c *fiber.Ctx) error {
		if strings.TrimSpace(c.Params("captchaId")) == "" {
			return httpx.Error(c, fiber.StatusBadRequest, "invalid_request", "请提供验证码标识")
		}
		image, err := service.CaptchaImage(context.Background(), c.Params("captchaId"))
		if err != nil {
			return httpx.Error(c, fiber.StatusInternalServerError, "internal_server_error")
		}
		if len(image) == 0 {
			return httpx.Error(c, fiber.StatusNotFound, "captcha_not_found")
		}
		c.Type("jpeg")
		return c.Send(image)
	})
}

func loginError(c *fiber.Ctx, err error) error {
	var captchaErr CaptchaRequiredError
	if errors.As(err, &captchaErr) {
		message := strings.TrimSpace(captchaErr.Message)
		if message == "" {
			message = "需要验证码"
		}
		return c.Status(fiber.StatusUnprocessableEntity).JSON(CaptchaRequiredResponse{
			Captcha:   captchaErr.Captcha,
			Execution: captchaErr.Execution,
			Message:   message,
		})
	}
	if errors.Is(err, ErrInvalidToken) {
		return httpx.Error(c, fiber.StatusUnauthorized, "invalid_credentials")
	}
	if errors.Is(err, ErrUpstreamTimeout) {
		return httpx.Error(c, fiber.StatusServiceUnavailable, "auth_upstream_timeout")
	}
	return httpx.Error(c, fiber.StatusInternalServerError, "internal_server_error")
}
