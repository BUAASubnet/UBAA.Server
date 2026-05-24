package announcement

import (
	"encoding/json"
	"os"
	"strings"

	"github.com/gofiber/fiber/v2"
)

type AppAnnouncement struct {
	ID          string  `json:"id"`
	Title       string  `json:"title"`
	Content     string  `json:"content"`
	ConfirmText *string `json:"confirmText"`
	LinkURL     *string `json:"linkUrl"`
}

type configFile struct {
	Enabled     bool    `json:"enabled"`
	ID          *string `json:"id"`
	Title       *string `json:"title"`
	Content     *string `json:"content"`
	ConfirmText *string `json:"confirmText"`
	LinkURL     *string `json:"linkUrl"`
}

func RegisterRoutes(app fiber.Router, path string) {
	app.Get("/api/v1/app/announcement", func(c *fiber.Ctx) error {
		announcement, ok := currentAnnouncement(path)
		if !ok {
			return c.SendStatus(fiber.StatusNoContent)
		}
		return c.Status(fiber.StatusOK).JSON(announcement)
	})
}

func currentAnnouncement(path string) (AppAnnouncement, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return AppAnnouncement{}, false
	}
	var cfg configFile
	if err := json.Unmarshal(raw, &cfg); err != nil || !cfg.Enabled {
		return AppAnnouncement{}, false
	}
	id := trimPtr(cfg.ID)
	title := trimPtr(cfg.Title)
	content := trimPtr(cfg.Content)
	if id == "" || title == "" || content == "" {
		return AppAnnouncement{}, false
	}
	return AppAnnouncement{
		ID:          id,
		Title:       title,
		Content:     content,
		ConfirmText: optionalTrimmed(cfg.ConfirmText),
		LinkURL:     optionalTrimmed(cfg.LinkURL),
	}, true
}

func trimPtr(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func optionalTrimmed(value *string) *string {
	trimmed := trimPtr(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
