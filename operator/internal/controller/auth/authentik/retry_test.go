package authentik

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"
)

func TestIsRetryableAPIError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "method not allowed", err: APIError{StatusCode: http.StatusMethodNotAllowed}, want: true},
		{name: "request timeout", err: APIError{StatusCode: http.StatusRequestTimeout}, want: true},
		{name: "conflict", err: APIError{StatusCode: http.StatusConflict}, want: true},
		{name: "rate limited", err: APIError{StatusCode: http.StatusTooManyRequests}, want: true},
		{name: "server error", err: APIError{StatusCode: http.StatusServiceUnavailable}, want: true},
		{name: "deadline", err: context.DeadlineExceeded, want: true},
		{name: "network", err: &net.DNSError{Err: "temporary lookup failure", IsTemporary: true}, want: true},
		{name: "bad request", err: APIError{StatusCode: http.StatusBadRequest}, want: false},
		{name: "unauthorized", err: APIError{StatusCode: http.StatusUnauthorized}, want: false},
		{name: "forbidden", err: APIError{StatusCode: http.StatusForbidden}, want: false},
		{name: "not found", err: APIError{StatusCode: http.StatusNotFound}, want: false},
		{name: "cancelled", err: context.Canceled, want: false},
		{name: "validation", err: errors.New("invalid response"), want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := IsRetryableAPIError(test.err); got != test.want {
				t.Fatalf("IsRetryableAPIError(%v) = %t, want %t", test.err, got, test.want)
			}
		})
	}
}
