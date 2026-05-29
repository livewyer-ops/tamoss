package rustfs

import "testing"

func TestMinioEndpointParsing(t *testing.T) {
	tests := []struct {
		name       string
		raw        string
		want       string
		wantSecure bool
		wantErr    bool
	}{
		{name: "http", raw: "http://rustfs:9000", want: "rustfs:9000"},
		{name: "https", raw: "https://s3.example.com", want: "s3.example.com", wantSecure: true},
		{name: "missing scheme", raw: "rustfs:9000", wantErr: true},
		{name: "missing host", raw: "https://", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, secure, err := minioEndpoint(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for endpoint %q", tt.raw)
				}
				return
			}
			if err != nil {
				t.Fatalf("parse endpoint: %v", err)
			}
			if got != tt.want || secure != tt.wantSecure {
				t.Fatalf("expected %q secure=%t, got %q secure=%t", tt.want, tt.wantSecure, got, secure)
			}
		})
	}
}

func TestCORSConfig(t *testing.T) {
	config := corsConfig("https://app.tamoss.example.com")
	if len(config.CORSRules) != 1 {
		t.Fatalf("expected one CORS rule, got %#v", config.CORSRules)
	}
	rule := config.CORSRules[0]
	if len(rule.AllowedOrigin) != 1 || rule.AllowedOrigin[0] != "https://app.tamoss.example.com" {
		t.Fatalf("unexpected allowed origins: %#v", rule.AllowedOrigin)
	}
	if len(rule.AllowedHeader) != 1 || rule.AllowedHeader[0] != "*" {
		t.Fatalf("expected wildcard allowed header, got %#v", rule.AllowedHeader)
	}
}
