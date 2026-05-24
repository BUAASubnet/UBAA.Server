package buildinfo

import (
	"strconv"
	"strings"
)

const (
	DefaultVersion     = "1.7.3"
	DefaultVersionCode = "26"
)

var (
	Version     = DefaultVersion
	VersionCode = DefaultVersionCode
	Commit      = "unknown"
	BuildTime   = "unknown"
)

func VersionCodeInt() int {
	parsed, err := strconv.Atoi(strings.TrimSpace(VersionCode))
	if err != nil || parsed < 0 {
		return 0
	}
	return parsed
}
