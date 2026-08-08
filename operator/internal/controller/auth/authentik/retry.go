package authentik

import (
	"context"
	"errors"
	"net"
	"net/http"
)

// IsRetryableAPIError reports whether an Authentik operation should receive a startup grace period.
func IsRetryableAPIError(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var apiError APIError
	if errors.As(err, &apiError) {
		switch apiError.StatusCode {
		case http.StatusMethodNotAllowed,
			http.StatusRequestTimeout,
			http.StatusConflict,
			http.StatusTooEarly,
			http.StatusTooManyRequests:
			return true
		default:
			return apiError.StatusCode >= http.StatusInternalServerError
		}
	}
	var networkError net.Error
	return errors.As(err, &networkError)
}
