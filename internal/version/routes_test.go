package version

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/BUAASubnet/UBAA.Server/internal/config"
	"github.com/gofiber/fiber/v2"
)

func TestCompareVersionsMatchesKtorBehavior(t *testing.T) {
	cases := []struct {
		left  string
		right string
		want  int
	}{
		{"v1.5.0", "1.5.0", 0},
		{"1.4.9", "1.5.0", -1},
		{"1.6.0", "1.5.9", 1},
		{"1.5", "1.5.0", 0},
	}
	for _, tc := range cases {
		got := compareVersions(tc.left, tc.right)
		if tc.want == 0 && got != 0 {
			t.Fatalf("compareVersions(%q, %q) = %d", tc.left, tc.right, got)
		}
		if tc.want < 0 && got >= 0 {
			t.Fatalf("compareVersions(%q, %q) = %d", tc.left, tc.right, got)
		}
		if tc.want > 0 && got <= 0 {
			t.Fatalf("compareVersions(%q, %q) = %d", tc.left, tc.right, got)
		}
	}
}

func TestVersionRouteReturnsAlignedServerVersionMetadata(t *testing.T) {
	app := fiber.New()
	RegisterRoutes(app, config.Config{
		ServerVersion:     "1.7.3",
		ServerVersionCode: 26,
		ServerCommit:      "abc1234",
		ServerBuildTime:   "2026-05-24T12:00:00Z",
		UpdateDownloadURL: "https://github.com/BUAASubnet/UBAA/releases",
	})

	resp, err := app.Test(httptest.NewRequest("GET", "/api/v1/app/version?clientVersion=1.7.2", nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var body AppVersionCheckResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Status != "UPDATE_AVAILABLE" || !body.UpdateAvailable || body.LatestVersion != "1.7.3" {
		t.Fatalf("body = %#v", body)
	}
	if body.VersionCode == nil || *body.VersionCode != 26 {
		t.Fatalf("versionCode = %#v", body.VersionCode)
	}
	if body.ServerCommit == nil || *body.ServerCommit != "abc1234" {
		t.Fatalf("serverCommit = %#v", body.ServerCommit)
	}
	if body.ServerBuildTime == nil || *body.ServerBuildTime != "2026-05-24T12:00:00Z" {
		t.Fatalf("serverBuildTime = %#v", body.ServerBuildTime)
	}
}
