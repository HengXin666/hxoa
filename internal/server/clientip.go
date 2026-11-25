package server

import (
	"net"
	"net/http"
	"strings"
)

// Cloudflare edge ranges, https://www.cloudflare.com/ips/ (2026-11).
var cloudflareNets = func() []*net.IPNet {
	var out []*net.IPNet
	for _, c := range []string{
		"173.245.48.0/20", "103.21.244.0/22", "103.22.200.0/22", "103.31.4.0/22",
		"141.101.64.0/18", "108.162.192.0/18", "190.93.240.0/20", "188.114.96.0/20",
		"197.234.240.0/22", "198.41.128.0/17", "162.158.0.0/15", "104.16.0.0/13",
		"104.24.0.0/14", "172.64.0.0/13", "131.0.72.0/22",
		"2400:cb00::/32", "2606:4700::/32", "2803:f800::/32", "2405:b500::/32",
		"2405:8100::/32", "2a06:98c0::/29", "2c0f:f248::/32",
	} {
		_, n, _ := net.ParseCIDR(c)
		out = append(out, n)
	}
	return out
}()

func isCloudflare(ip string) bool {
	p := net.ParseIP(ip)
	if p == nil {
		return false
	}
	for _, n := range cloudflareNets {
		if n.Contains(p) {
			return true
		}
	}
	return false
}

func isLocalPeer(ip string) bool {
	p := net.ParseIP(ip)
	return p != nil && (p.IsLoopback() || p.IsPrivate())
}

// clientIP is the address login limits and logs are keyed on. Headers are
// only believed when they come from a hop that can vouch for them, so a
// caller that reaches the origin directly cannot pick its own address:
//   - the peer, or the last X-Forwarded-For hop a local proxy (SafeLine,
//     HX-Webx) recorded, is the address that actually connected;
//   - Cf-Connecting-Ip is used only when that address is a Cloudflare edge,
//     or when a local proxy (cloudflared) forwarded without adding a hop.
func clientIP(r *http.Request) string {
	peer, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		peer = r.RemoteAddr
	}
	cf := strings.TrimSpace(r.Header.Get("Cf-Connecting-Ip"))
	hop := peer
	if isLocalPeer(peer) {
		xff := r.Header.Get("X-Forwarded-For")
		if xff == "" {
			if cf != "" {
				return cf
			}
			return peer
		}
		parts := strings.Split(xff, ",")
		hop = strings.TrimSpace(parts[len(parts)-1])
	}
	if cf != "" && isCloudflare(hop) {
		return cf
	}
	return hop
}
