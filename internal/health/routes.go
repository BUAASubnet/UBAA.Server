package health

import (
	"github.com/BUAASubnet/UBAA.Server/internal/config"
	"github.com/BUAASubnet/UBAA.Server/internal/dto"
	"github.com/BUAASubnet/UBAA.Server/internal/storage"
	"github.com/gofiber/fiber/v2"
)

func RegisterRoutes(app fiber.Router, cfg config.Config, db *storage.DB) {
	app.Get("/health/live", func(c *fiber.Ctx) error {
		return c.Status(fiber.StatusOK).JSON(dto.HealthCheckResponse{
			Status:     "up",
			InstanceID: cfg.InstanceID,
			Checks:     map[string]string{"application": "up"},
		})
	})
	app.Get("/health/ready", func(c *fiber.Ctx) error {
		status := fiber.StatusOK
		ready := "up"
		if err := db.PingContext(c.Context()); err != nil {
			status = fiber.StatusServiceUnavailable
			ready = "down"
		}
		bodyStatus := "ready"
		if ready != "up" {
			bodyStatus = "degraded"
		}
		return c.Status(status).JSON(dto.HealthCheckResponse{
			Status:     bodyStatus,
			InstanceID: cfg.InstanceID,
			Checks:     map[string]string{"redis": ready},
		})
	})
}
