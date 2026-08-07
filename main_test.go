package main

import (
	"errors"
	"testing"
)

func TestIsAllowedOriginAllowsLocalhostAndLoopback(t *testing.T) {

	cases := []struct {
		name   string
		origin string
		want   bool
	}{
		{
			name:   "vite dev server",
			origin: "http://localhost:5173",
			want:   true,
		},
		{
			name:   "loopback dev server",
			origin: "http://127.0.0.1:5173",
			want:   true,
		},
		{
			name:   "production client",
			origin: "https://startup-client.netlify.app",
			want:   true,
		},
		{
			name:   "blocked origin",
			origin: "https://example.com",
			want:   false,
		},
	}

	for _, tc := range cases {

		t.Run(tc.name, func(t *testing.T) {

			got := isAllowedOrigin(tc.origin)

			if got != tc.want {

				t.Fatalf(
					"isAllowedOrigin(%q) = %v, want %v",
					tc.origin,
					got,
					tc.want,
				)
			}
		})
	}
}

func TestIsEmailConfigurationError(t *testing.T) {

	err := errors.New(
		"email configuration is incomplete: EMAIL, APP, and ADMIN must be set",
	)

	if !isEmailConfigurationError(err) {
		t.Fatalf("expected configuration error to be recognized")
	}
}