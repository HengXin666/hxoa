package server

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

// Many desktop machines run a TUN proxy (Clash/mihomo, sing-box ...) in
// fake-ip mode. Cloudflare's edge names then resolve to 198.18.x.x and
// cloudflared's QUIC dials time out forever. We detect that and launch
// cloudflared the way that survives it: HTTP/2, real edge IPs, and the
// physical NIC as source address.

var fakeNet = &net.IPNet{IP: net.IPv4(198, 18, 0, 0), Mask: net.CIDRMask(15, 32)}

// Known-good Cloudflare tunnel edge addresses, used when DNS is hijacked
// and DoH is unavailable too.
var staticEdges = []string{"198.41.192.227", "198.41.192.167", "198.41.200.193", "198.41.200.233"}

type netEnv struct {
	FakeIP bool     // edge names resolve into 198.18.0.0/15
	Bind   string   // physical NIC address to dial from
	Edges  []string // ip:7844
}

func probeNetwork(ctx context.Context) netEnv {
	var e netEnv
	ips, _ := net.DefaultResolver.LookupIP(ctx, "ip4", "region1.v2.argotunnel.com")
	for _, ip := range ips {
		if fakeNet.Contains(ip) {
			e.FakeIP = true
		}
	}
	if !e.FakeIP {
		return e
	}
	e.Bind = physicalAddr()
	for _, name := range []string{"region1.v2.argotunnel.com", "region2.v2.argotunnel.com"} {
		for _, ip := range dohA(ctx, name) {
			e.Edges = append(e.Edges, ip+":7844")
		}
	}
	if len(e.Edges) == 0 {
		for _, ip := range staticEdges {
			e.Edges = append(e.Edges, ip+":7844")
		}
	}
	return e
}

// env returns cloudflared environment for this network. HTTP/2 always:
// it passes through proxies and firewalls that drop UDP 7844.
func (e netEnv) env() []string {
	out := []string{"TUNNEL_TRANSPORT_PROTOCOL=http2"}
	if e.FakeIP {
		out = append(out, "TUNNEL_NO_PRECHECKS=true")
		if len(e.Edges) > 0 {
			out = append(out, "TUNNEL_EDGE="+strings.Join(e.Edges, ","))
		}
		if e.Bind != "" {
			out = append(out, "TUNNEL_EDGE_BIND_ADDRESS="+e.Bind)
		}
	}
	return out
}

func (e netEnv) describe() string {
	if !e.FakeIP {
		return "HTTP/2 直连"
	}
	s := "检测到本机代理的 fake-ip, 已绕开: HTTP/2"
	if e.Bind != "" {
		s += ", 从网卡 " + e.Bind + " 直连"
	}
	return s
}

// dohA asks Cloudflare's DNS-over-HTTPS for real A records.
func dohA(ctx context.Context, name string) []string {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://cloudflare-dns.com/dns-query?type=A&name="+name, nil)
	req.Header.Set("Accept", "application/dns-json")
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil
	}
	defer r.Body.Close()
	var j struct {
		Answer []struct {
			Type int    `json:"type"`
			Data string `json:"data"`
		}
	}
	json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&j)
	var out []string
	for _, a := range j.Answer {
		if ip := net.ParseIP(a.Data); a.Type == 1 && ip != nil && !fakeNet.Contains(ip) {
			out = append(out, a.Data)
		}
	}
	return out
}

// physicalAddr is the IPv4 address of the interface behind the main
// table's default route that is not a TUN device.
func physicalAddr() string {
	f, err := os.Open("/proc/net/route")
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fs := strings.Fields(sc.Text())
		if len(fs) < 3 || fs[1] != "00000000" {
			continue
		}
		name := fs[0]
		if strings.HasPrefix(name, "tun") || strings.HasPrefix(name, "utun") || strings.Contains(name, "Meta") || strings.Contains(name, "clash") || strings.HasPrefix(name, "singbox") {
			continue
		}
		ifc, err := net.InterfaceByName(name)
		if err != nil {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok && ipn.IP.To4() != nil && !ipn.IP.IsLoopback() {
				return ipn.IP.String()
			}
		}
	}
	return ""
}

// Diagnosis turns cloudflared's log into one plain sentence, or "".
func diagnose(logs []string, state string) string {
	if state == "running" {
		return ""
	}
	tail := logs
	if len(tail) > 30 {
		tail = tail[len(tail)-30:]
	}
	has := func(s string) bool {
		for _, l := range tail {
			if strings.Contains(l, s) {
				return true
			}
		}
		return false
	}
	switch {
	case has("下载 cloudflared 失败"):
		return "cloudflared 下载失败, 检查这台机器能否访问 GitHub。"
	case has("status 429"):
		return "Cloudflare 限流了快速隧道 (429), 等几分钟会自动重试, 或改用固定域名。"
	case has("quick tunnel provisioning failed"), has("failed to request quick Tunnel"):
		return "向 trycloudflare.com 申请快速隧道失败, 稍后会自动重试。长期使用建议配固定域名。"
	case has("198.18."):
		return "本机代理 (TUN fake-ip) 拦住了到 Cloudflare 的连接。hxoad 会自动改用 HTTP/2 和网卡直连, 重启隧道即可。"
	case has("failed to dial to edge with quic"), has("UDP Connectivity"):
		return "网络挡住了 UDP (QUIC), 正在改用 HTTP/2 重试。"
	case has("TLS handshake with edge"):
		return "连得上 Cloudflare 但握手失败, 多半是代理或防火墙在中间截断, 稍后会自动重试。"
	case has("Unauthorized"), has("invalid token"), has("Invalid tunnel secret"):
		return "隧道 token 无效或隧道已被删除, 重新配置固定域名。"
	case has("status 429"), has("429 Too Many Requests"), has("rate limit"):
		return "Cloudflare 限流了快速隧道, 等几分钟再试, 或改用固定域名。"
	case has("no such host"), has("lookup "):
		return "DNS 解析失败, 这台机器现在可能没联网。"
	}
	return ""
}

type CheckStep struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
	MS     int64  `json:"ms,omitempty"`
}

// Check verifies the path an AI client takes: the local agent API, then
// the public URL end to end. A healthy agent answers /v1/hello without a
// token with 401 and code "auth".
func (m *TunnelManager) Check(ctx context.Context, public string) []CheckStep {
	steps := []CheckStep{}
	probe := func(url string) (int, string, int64, error) {
		ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
		defer cancel()
		req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
		req.Header.Set("User-Agent", "hxoa/2") // the client's UA; some zones block others
		t0 := time.Now()
		r, err := http.DefaultClient.Do(req)
		ms := time.Since(t0).Milliseconds()
		if err != nil {
			return 0, "", ms, err
		}
		defer r.Body.Close()
		b, _ := io.ReadAll(io.LimitReader(r.Body, 4096))
		return r.StatusCode, string(b), ms, nil
	}
	isAgent := func(code int, body string) bool {
		return code == 401 && strings.Contains(body, `"code"`) && strings.Contains(body, "令牌")
	}

	code, body, ms, err := probe(m.target + "/v1/hello")
	switch {
	case err != nil:
		steps = append(steps, CheckStep{Name: "本机 agent", Detail: "连不上 " + m.target + ": " + shortErr(err)})
		return steps
	case !isAgent(code, body):
		steps = append(steps, CheckStep{Name: "本机 agent", Detail: fmt.Sprintf("%s 返回 %d, 不像 hxoad", m.target, code)})
		return steps
	default:
		steps = append(steps, CheckStep{Name: "本机 agent", OK: true, Detail: m.target + " 正常", MS: ms})
	}

	if public == "" {
		steps = append(steps, CheckStep{Name: "公网地址", Detail: "还没有公网地址, 先开启隧道或配置固定域名"})
		return steps
	}
	code, body, ms, err = probe(strings.TrimRight(public, "/") + "/v1/hello")
	s := CheckStep{Name: "公网访问", MS: ms}
	switch {
	case err != nil:
		s.Detail = "访问 " + public + " 失败: " + shortErr(err)
	case isAgent(code, body):
		s.OK, s.Detail = true, "从公网能连到这台 hxoad"
	case code == 530 || strings.Contains(body, "1033"):
		s.Detail = "Cloudflare 找不到在线的隧道 (530/1033): cloudflared 没连上或已退出"
	case code == 404:
		s.Detail = "域名通了, 但 /v1/ 没转到 agent (" + strings.TrimPrefix(m.target, "http://") + ")"
	case code == 403:
		s.Detail = "被防火墙或 WAF 拦截 (403), 给 hxoa 客户端放行 /v1/"
	case code == 502 || code == 503 || code == 504:
		s.Detail = fmt.Sprintf("反代或隧道连不上本机 agent (%d)", code)
	default:
		s.Detail = fmt.Sprintf("返回 %d, 不是 hxoad", code)
	}
	return append(steps, s)
}

func shortErr(err error) string {
	s := err.Error()
	if i := strings.LastIndex(s, ": "); i >= 0 && len(s) > 80 {
		s = s[i+2:]
	}
	if strings.Contains(s, "deadline exceeded") || strings.Contains(s, "Timeout") {
		return "超时"
	}
	return s
}
