package features

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
)

type upstreamTimeoutError struct {
	code string
}

func (e upstreamTimeoutError) Error() string {
	if e.code == "" {
		return "upstream timeout"
	}
	return e.code
}

func timeoutError(code string) error {
	return upstreamTimeoutError{code: code}
}

func timeoutCode(err error) (string, bool) {
	var timeout upstreamTimeoutError
	if errors.As(err, &timeout) {
		if strings.TrimSpace(timeout.code) == "" {
			return "upstream_timeout", true
		}
		return timeout.code, true
	}
	return "", false
}

func errorIsTimeout(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func upstreamRequestError(err error, fallback error, timeoutCode string) error {
	if errorIsTimeout(err) {
		return timeoutError(timeoutCode)
	}
	return fallback
}

func upstreamStatusError(status int, fallback error, timeoutCode string) error {
	if status == http.StatusGatewayTimeout {
		return timeoutError(timeoutCode)
	}
	return fallback
}
