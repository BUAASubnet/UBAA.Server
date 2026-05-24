package app

import (
	"context"
	"fmt"
	"runtime"
	"strings"

	"github.com/BUAASubnet/UBAA.Server/internal/features"
)

type gaugeSample struct {
	name  string
	help  string
	value float64
}

func renderMetrics(ctx context.Context, deps Dependencies) string {
	var sizes features.CacheSizes
	if deps.Features != nil {
		sizes = deps.Features.CacheSizes()
	}
	redisReady := 0.0
	if deps.DB != nil && deps.DB.PingContext(ctx) == nil {
		redisReady = 1
	}
	activeSessions := 0
	activePreLogins := 0
	if deps.Auth != nil {
		activeSessions = deps.Auth.ActiveSessionCount(ctx)
		activePreLogins = deps.Auth.ActivePreLoginSessionCount(ctx)
	}
	samples := []gaugeSample{
		{name: "ubaa_sessions_active", help: "Active authenticated sessions.", value: float64(activeSessions)},
		{name: "ubaa_sessions_prelogin", help: "Active pre-login sessions.", value: float64(activePreLogins)},
		{name: "ubaa_signin_cache", help: "Cached iclass sign-in clients.", value: float64(sizes.Signin)},
		{name: "ubaa_bykc_cache", help: "Cached BYKC clients.", value: float64(sizes.Bykc)},
		{name: "ubaa_cgyy_cache", help: "Cached CGYY clients.", value: float64(sizes.Cgyy)},
		{name: "ubaa_spoc_cache", help: "Cached SPOC clients.", value: float64(sizes.Spoc)},
		{name: "ubaa_judge_cache", help: "Cached Judge clients.", value: float64(sizes.Judge)},
		{name: "ubaa_libbook_cache", help: "Cached LibBook clients.", value: float64(sizes.LibBook)},
		{name: "ubaa_ygdk_cache", help: "Cached YGDK clients.", value: float64(sizes.Ygdk)},
		{name: "ubaa_ygdk_context_cache", help: "Cached YGDK overview contexts.", value: float64(sizes.YgdkContext)},
		{name: "ubaa_redis_ready", help: "Storage readiness, kept under the original Ktor metric name.", value: redisReady},
		{name: "ubaa_sqlite_ready", help: "SQLite storage readiness.", value: redisReady},
		{name: "go_goroutines", help: "Number of goroutines that currently exist.", value: float64(runtime.NumGoroutine())},
	}

	var out strings.Builder
	out.WriteString("# UBAA.Server metrics\n")
	for _, sample := range samples {
		fmt.Fprintf(&out, "# HELP %s %s\n", sample.name, sample.help)
		fmt.Fprintf(&out, "# TYPE %s gauge\n", sample.name)
		fmt.Fprintf(&out, "%s %g\n", sample.name, sample.value)
	}
	return out.String()
}
