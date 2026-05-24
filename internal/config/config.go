package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type AllowedCorsOrigin struct {
	Raw     string
	Host    string
	Schemes []string
}

type Config struct {
	ServerHost string
	ServerPort int

	InstanceID             string
	EnableForwardedHeaders bool
	CorsAllowedOrigins     []AllowedCorsOrigin
	AllowAnyCorsHost       bool

	JWTSecret       string
	AccessTokenTTL  time.Duration
	RefreshTokenTTL time.Duration
	SessionTTL      time.Duration
	PreLoginTTL     time.Duration

	LoginMaxConcurrency   int
	AuthValidationTimeout time.Duration
	AuthPreloadTimeout    time.Duration
	AuthLoginTimeout      time.Duration

	SQLitePath string

	ServerVersion     string
	UpdateDownloadURL string

	FreeCacheSizeBytes int

	UseVPN                   bool
	TrustAllCerts            bool
	BykcDebugRawAPILog       bool
	BykcDebugParsedCourseLog bool
}

func Load() Config {
	_ = godotenv.Load()
	hostname, _ := os.Hostname()
	instanceID := firstNonBlank(env("INSTANCE_ID"), env("HOSTNAME"), hostname)
	if instanceID == "" {
		instanceID = fmt.Sprintf("pid-%d", os.Getpid())
	}
	originsRaw := splitCSV(env("CORS_ALLOWED_ORIGINS"))
	origins := make([]AllowedCorsOrigin, 0, len(originsRaw))
	allowAny := false
	for _, raw := range originsRaw {
		if raw == "*" {
			allowAny = true
			continue
		}
		if parsed, ok := parseOrigin(raw); ok {
			origins = append(origins, parsed)
		}
	}
	return Config{
		ServerHost:             stringEnv("SERVER_BIND_HOST", "0.0.0.0"),
		ServerPort:             intEnv("SERVER_PORT", 5432),
		InstanceID:             instanceID,
		EnableForwardedHeaders: boolEnv("ENABLE_FORWARDED_HEADERS", true),
		CorsAllowedOrigins:     origins,
		AllowAnyCorsHost:       allowAny,
		JWTSecret:              stringEnv("JWT_SECRET", "ubaa-dev-secret-unsafe"),
		AccessTokenTTL:         time.Duration(intEnv("ACCESS_TOKEN_TTL_MINUTES", 30)) * time.Minute,
		RefreshTokenTTL:        time.Duration(intEnv("REFRESH_TOKEN_TTL_DAYS", 7)) * 24 * time.Hour,
		SessionTTL:             time.Duration(intEnv("SESSION_TTL_DAYS", 7)) * 24 * time.Hour,
		PreLoginTTL:            time.Duration(max(intEnv("PRELOGIN_TTL_MINUTES", 5), 1)) * time.Minute,
		LoginMaxConcurrency:    max(intEnv("LOGIN_MAX_CONCURRENCY", 6), 1),
		AuthValidationTimeout:  millisEnv("AUTH_VALIDATION_TIMEOUT_MS", 3000),
		AuthPreloadTimeout:     millisEnv("AUTH_PRELOAD_TIMEOUT_MS", 3000),
		AuthLoginTimeout:       millisEnv("AUTH_LOGIN_TIMEOUT_MS", 18000),
		SQLitePath:             stringEnv("SQLITE_PATH", "data/ubaa-server.db"),
		ServerVersion:          firstNonBlank(env("UBAA_SERVER_VERSION"), env("PROJECT_VERSION"), "unknown"),
		UpdateDownloadURL: stringEnv(
			"UPDATE_DOWNLOAD_URL",
			"https://github.com/BUAASubnet/UBAA/releases",
		),
		FreeCacheSizeBytes:       max(intEnv("FREECACHE_SIZE_MB", 64), 1) * 1024 * 1024,
		UseVPN:                   boolEnv("USE_VPN", false),
		TrustAllCerts:            boolEnv("TRUST_ALL_CERTS", false),
		BykcDebugRawAPILog:       boolEnv("BYKC_DEBUG_RAW_API_LOG", false),
		BykcDebugParsedCourseLog: boolEnv("BYKC_DEBUG_PARSED_COURSE_LOG", false),
	}
}

func (c Config) ListenAddr() string {
	return fmt.Sprintf("%s:%d", c.ServerHost, c.ServerPort)
}

func env(name string) string {
	return strings.TrimSpace(os.Getenv(name))
}

func stringEnv(name, fallback string) string {
	if value := env(name); value != "" {
		return value
	}
	return fallback
}

func intEnv(name string, fallback int) int {
	value := env(name)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func millisEnv(name string, fallback int) time.Duration {
	return time.Duration(max(intEnv(name, fallback), 1)) * time.Millisecond
}

func boolEnv(name string, fallback bool) bool {
	value := env(name)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func splitCSV(value string) []string {
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

func parseOrigin(raw string) (AllowedCorsOrigin, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return AllowedCorsOrigin{}, false
	}
	return AllowedCorsOrigin{Raw: raw, Host: u.Host, Schemes: []string{u.Scheme}}, true
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func max(left, right int) int {
	if left > right {
		return left
	}
	return right
}
