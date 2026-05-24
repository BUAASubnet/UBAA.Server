package app

import (
	"strings"

	"github.com/BUAASubnet/UBAA.Server/internal/announcement"
	"github.com/BUAASubnet/UBAA.Server/internal/auth"
	"github.com/BUAASubnet/UBAA.Server/internal/config"
	"github.com/BUAASubnet/UBAA.Server/internal/features"
	"github.com/BUAASubnet/UBAA.Server/internal/health"
	"github.com/BUAASubnet/UBAA.Server/internal/httpx"
	"github.com/BUAASubnet/UBAA.Server/internal/storage"
	"github.com/BUAASubnet/UBAA.Server/internal/version"
	"github.com/coocood/freecache"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/requestid"
)

type Dependencies struct {
	Config   config.Config
	DB       *storage.DB
	Cache    *freecache.Cache
	Auth     *auth.Service
	Features *features.Service
}

func New(deps Dependencies) *fiber.App {
	cfg := deps.Config
	app := fiber.New(fiber.Config{
		ErrorHandler: httpx.FiberErrorHandler,
	})
	app.Use(requestid.New(requestid.Config{
		Header: fiber.HeaderXRequestID,
	}))
	app.Use(cors.New(cors.Config{
		AllowOriginsFunc: func(origin string) bool {
			return isAllowedOrigin(origin, cfg)
		},
		AllowMethods: strings.Join([]string{
			fiber.MethodGet,
			fiber.MethodPost,
			fiber.MethodPut,
			fiber.MethodDelete,
			fiber.MethodPatch,
			fiber.MethodOptions,
		}, ","),
		AllowHeaders: strings.Join([]string{
			fiber.HeaderAuthorization,
			fiber.HeaderContentType,
			fiber.HeaderAccessControlAllowOrigin,
			fiber.HeaderXRequestID,
		}, ","),
	}))

	app.Get("/metrics", func(c *fiber.Ctx) error {
		c.Set(fiber.HeaderContentType, "text/plain; version=0.0.4; charset=utf-8")
		return c.SendString(renderMetrics(c.Context(), deps))
	})
	health.RegisterRoutes(app, cfg, deps.DB)
	version.RegisterRoutes(app, cfg)
	announcement.RegisterRoutes(app, "announcement.json")
	auth.RegisterRoutes(app, deps.Auth)

	api := app.Group("/api/v1", auth.Middleware(deps.Auth))
	features.RegisterRoutes(api, deps.Features)

	app.Get("/", func(c *fiber.Ctx) error {
		return c.SendString("Ktor: Hello, Java!")
	})
	return app
}

func isAllowedOrigin(origin string, cfg config.Config) bool {
	if origin == "" || cfg.AllowAnyCorsHost {
		return true
	}
	for _, allowed := range cfg.CorsAllowedOrigins {
		if origin == allowed.Raw {
			return true
		}
	}
	return len(cfg.CorsAllowedOrigins) == 0
}
