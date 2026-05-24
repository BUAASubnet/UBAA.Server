package auth

import (
	"context"

	"github.com/BUAASubnet/UBAA.Server/internal/httpx"
	"github.com/gofiber/fiber/v2"
)

const SessionLocalKey = "ubaa-session"

func Middleware(service *Service) fiber.Handler {
	return func(c *fiber.Ctx) error {
		session, err := service.ValidateBearer(context.Background(), c.Get(fiber.HeaderAuthorization), true)
		if err != nil || session == nil {
			return httpx.Error(c, fiber.StatusUnauthorized, "invalid_token")
		}
		c.Locals(SessionLocalKey, session)
		return c.Next()
	}
}

func CurrentSession(c *fiber.Ctx) *Session {
	session, _ := c.Locals(SessionLocalKey).(*Session)
	return session
}

func RequireSession(c *fiber.Ctx) (*Session, error) {
	session := CurrentSession(c)
	if session == nil {
		return nil, httpx.Error(c, fiber.StatusUnauthorized, "invalid_token")
	}
	return session, nil
}
