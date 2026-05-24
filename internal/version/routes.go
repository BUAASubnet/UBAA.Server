package version

import (
	"strconv"
	"strings"

	"github.com/BUAASubnet/UBAA.Server/internal/config"
	"github.com/BUAASubnet/UBAA.Server/internal/httpx"
	"github.com/gofiber/fiber/v2"
)

type AppVersionCheckResponse struct {
	LatestVersion   string  `json:"latestVersion"`
	Status          string  `json:"status"`
	UpdateAvailable bool    `json:"updateAvailable"`
	DownloadURL     string  `json:"downloadUrl"`
	ReleaseNotes    *string `json:"releaseNotes"`
	ServerVersion   *string `json:"serverVersion"`
	VersionCode     *int    `json:"versionCode,omitempty"`
	ServerCommit    *string `json:"serverCommit,omitempty"`
	ServerBuildTime *string `json:"serverBuildTime,omitempty"`
	Aligned         *bool   `json:"aligned"`
}

func RegisterRoutes(app fiber.Router, cfg config.Config) {
	app.Get("/api/v1/app/version", func(c *fiber.Ctx) error {
		clientVersion := c.Query("clientVersion")
		if strings.TrimSpace(clientVersion) == "" {
			return httpx.Error(c, fiber.StatusBadRequest, "missing_client_version")
		}
		status := "UP_TO_DATE"
		updateAvailable := false
		if !knownVersion(cfg.ServerVersion) {
			status = "UNKNOWN_LATEST_VERSION"
		} else if compareVersions(clientVersion, cfg.ServerVersion) < 0 {
			status = "UPDATE_AVAILABLE"
			updateAvailable = true
		}
		aligned := !updateAvailable
		serverVersion := cfg.ServerVersion
		versionCode := cfg.ServerVersionCode
		serverCommit := knownMetadata(cfg.ServerCommit)
		serverBuildTime := knownMetadata(cfg.ServerBuildTime)
		return c.Status(fiber.StatusOK).JSON(AppVersionCheckResponse{
			LatestVersion:   cfg.ServerVersion,
			Status:          status,
			UpdateAvailable: updateAvailable,
			DownloadURL:     cfg.UpdateDownloadURL,
			ReleaseNotes:    nil,
			ServerVersion:   &serverVersion,
			VersionCode:     &versionCode,
			ServerCommit:    serverCommit,
			ServerBuildTime: serverBuildTime,
			Aligned:         &aligned,
		})
	})
}

func knownVersion(version string) bool {
	normalized := strings.TrimSpace(version)
	return normalized != "" && normalized != "unknown"
}

func knownMetadata(value string) *string {
	normalized := strings.TrimSpace(value)
	if normalized == "" || normalized == "unknown" {
		return nil
	}
	return &normalized
}

func compareVersions(left, right string) int {
	leftParts := versionParts(left)
	rightParts := versionParts(right)
	size := len(leftParts)
	if len(rightParts) > size {
		size = len(rightParts)
	}
	for i := 0; i < size; i++ {
		lv, rv := 0, 0
		if i < len(leftParts) {
			lv = leftParts[i]
		}
		if i < len(rightParts) {
			rv = rightParts[i]
		}
		if lv < rv {
			return -1
		}
		if lv > rv {
			return 1
		}
	}
	return 0
}

func versionParts(version string) []int {
	trimmed := strings.TrimPrefix(strings.TrimSpace(version), "v")
	parts := strings.Split(trimmed, ".")
	result := make([]int, 0, len(parts))
	for _, part := range parts {
		value, err := strconv.Atoi(part)
		if err != nil {
			value = 0
		}
		result = append(result, value)
	}
	return result
}
