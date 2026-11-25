package server

import (
	"net/http/httptest"
	"testing"
)

func TestClientIP(t *testing.T) {
	cases := []struct {
		remote, xff, cf, want string
	}{
		{"203.0.113.9:1", "", "1.2.3.4", "203.0.113.9"},                   // direct, spoofed header ignored
		{"127.0.0.1:1", "203.0.113.9", "1.2.3.4", "203.0.113.9"},          // via SafeLine, not from CF
		{"127.0.0.1:1", "1.2.3.4, 162.158.1.1", "1.2.3.4", "1.2.3.4"},     // via CF then SafeLine
		{"127.0.0.1:1", "9.9.9.9, 203.0.113.9", "9.9.9.9", "203.0.113.9"}, // forged XFF prefix
		{"127.0.0.1:1", "", "1.2.3.4", "1.2.3.4"},                         // local cloudflared
		{"127.0.0.1:1", "", "", "127.0.0.1"},
		{"162.158.1.1:1", "", "1.2.3.4", "1.2.3.4"}, // CF straight to hxoad
	}
	for _, c := range cases {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = c.remote
		if c.xff != "" {
			r.Header.Set("X-Forwarded-For", c.xff)
		}
		if c.cf != "" {
			r.Header.Set("Cf-Connecting-Ip", c.cf)
		}
		if got := clientIP(r); got != c.want {
			t.Errorf("%+v: got %s", c, got)
		}
	}
}
