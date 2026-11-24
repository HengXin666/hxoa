package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
)

// TunnelManager runs cloudflared for hxoad, so the user never installs or
// configures it by hand. Modes:
//   - quick:    free *.trycloudflare.com address, no account, changes on restart
//   - token:    a named tunnel (fixed hostname) run from its token
//   - external: cloudflared runs elsewhere (docker sidecar); we only read its URL
//   - off:      no tunnel (public URL set by flag, or LAN use)
type TunnelManager struct {
	store   *Store
	target  string // e.g. http://127.0.0.1:7070
	fixed   string // -public-url
	metrics string // external sidecar metrics URL

	mu      sync.Mutex
	cmd     *exec.Cmd
	stop    context.CancelFunc
	url     string
	state   string // stopped | downloading | starting | running | error
	lastErr string
	logs    []string
	gen     int
}

func NewTunnelManager(store *Store, agentAddr, fixed, metrics string) *TunnelManager {
	host, port, _ := net.SplitHostPort(agentAddr)
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	m := &TunnelManager{store: store, target: "http://" + net.JoinHostPort(host, port),
		fixed: strings.TrimRight(fixed, "/"), metrics: strings.TrimRight(metrics, "/"), state: "stopped"}
	if m.metrics != "" {
		go m.pollMetrics()
	}
	return m
}

// Start applies the saved config (called at boot and after every change).
func (m *TunnelManager) Start() {
	cfg := m.store.Tunnel()
	m.halt()
	switch cfg.Mode {
	case "quick":
		go m.supervise(cfg, []string{"tunnel", "--no-autoupdate", "--url", m.target})
	case "token":
		m.setURL("https://" + cfg.Hostname)
		go m.supervise(cfg, []string{"tunnel", "--no-autoupdate", "run"})
	}
}

func (m *TunnelManager) URL() string {
	if m.fixed != "" {
		return m.fixed
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.url
}

type TunnelStatus struct {
	Mode     string   `json:"mode"`
	State    string   `json:"state"`
	URL      string   `json:"url"`
	Hostname string   `json:"hostname,omitempty"`
	Error    string   `json:"error,omitempty"`
	Logs     []string `json:"logs"`
	Binary   string   `json:"binary,omitempty"`
	Fixed    bool     `json:"fixed,omitempty"`
}

func (m *TunnelManager) Status() TunnelStatus {
	cfg := m.store.Tunnel()
	m.mu.Lock()
	defer m.mu.Unlock()
	mode := cfg.Mode
	if mode == "" {
		mode = "off"
	}
	if m.metrics != "" {
		mode = "external"
	}
	st := TunnelStatus{Mode: mode, State: m.state, URL: m.url, Hostname: cfg.Hostname, Error: m.lastErr,
		Logs: append([]string{}, m.logs...), Fixed: m.fixed != ""}
	if m.fixed != "" {
		st.URL = m.fixed
	}
	st.Binary, _ = m.findBinary()
	return st
}

func (m *TunnelManager) setURL(u string) {
	m.mu.Lock()
	if m.url != u && u != "" {
		log.Printf("tunnel: %s", u)
	}
	m.url = u
	m.mu.Unlock()
}

func (m *TunnelManager) setState(st, errMsg string) {
	m.mu.Lock()
	m.state, m.lastErr = st, errMsg
	m.mu.Unlock()
}

func (m *TunnelManager) logLine(s string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.logs = append(m.logs, time.Now().Format("15:04:05 ")+s)
	if len(m.logs) > 60 {
		m.logs = m.logs[len(m.logs)-60:]
	}
}

func (m *TunnelManager) halt() {
	m.mu.Lock()
	m.gen++
	if m.stop != nil {
		m.stop()
		m.stop = nil
	}
	m.url = ""
	m.state = "stopped"
	m.lastErr = ""
	m.mu.Unlock()
}

var quickRe = regexp.MustCompile(`https://[a-z0-9-]+\.trycloudflare\.com`)

func (m *TunnelManager) supervise(cfg TunnelConfig, args []string) {
	m.mu.Lock()
	gen := m.gen
	ctx, cancel := context.WithCancel(context.Background())
	m.stop = cancel
	m.mu.Unlock()
	alive := func() bool {
		m.mu.Lock()
		defer m.mu.Unlock()
		return m.gen == gen && ctx.Err() == nil
	}
	backoff := 3 * time.Second
	for alive() {
		bin, err := m.ensureBinary(ctx)
		if err != nil {
			m.setState("error", err.Error())
			m.logLine("下载 cloudflared 失败: " + err.Error())
			select {
			case <-ctx.Done():
				return
			case <-time.After(30 * time.Second):
			}
			continue
		}
		m.setState("starting", "")
		cmd := exec.CommandContext(ctx, bin, args...)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM) }
		cmd.WaitDelay = 5 * time.Second
		cmd.Env = scrubEnv(os.Environ())
		if cfg.Mode == "token" {
			cmd.Env = append(cmd.Env, "TUNNEL_TOKEN="+cfg.Token) // not on argv, so ps cannot show it
		}
		stderr, _ := cmd.StderrPipe()
		cmd.Stdout = cmd.Stderr
		start := time.Now()
		if err := cmd.Start(); err != nil {
			m.setState("error", err.Error())
			return
		}
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			line := sc.Text()
			if cfg.Token != "" {
				line = strings.ReplaceAll(line, cfg.Token, "***")
			}
			m.logLine(trimLog(line))
			if u := quickRe.FindString(line); u != "" && cfg.Mode == "quick" {
				m.setURL(u)
				m.setState("running", "")
			}
			if strings.Contains(line, "Registered tunnel connection") {
				m.setState("running", "")
			}
		}
		err = cmd.Wait()
		if !alive() {
			return
		}
		msg := "cloudflared 退出"
		if err != nil {
			msg += ": " + err.Error()
		}
		m.setState("error", msg)
		if cfg.Mode == "quick" {
			m.setURL("")
		}
		if time.Since(start) > time.Minute {
			backoff = 3 * time.Second
		} else if backoff < time.Minute {
			backoff *= 2
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
	}
}

func trimLog(s string) string {
	// cloudflared lines start with a timestamp and level; keep the message.
	if i := strings.Index(s, " INF "); i >= 0 {
		return s[i+5:]
	}
	if i := strings.Index(s, " ERR "); i >= 0 {
		return "ERR " + s[i+5:]
	}
	if i := strings.Index(s, " WRN "); i >= 0 {
		return "WRN " + s[i+5:]
	}
	return s
}

func (m *TunnelManager) pollMetrics() {
	client := &http.Client{Timeout: 3 * time.Second}
	for {
		resp, err := client.Get(m.metrics + "/quicktunnel")
		if err == nil {
			var v struct {
				Hostname string `json:"hostname"`
			}
			json.NewDecoder(resp.Body).Decode(&v)
			resp.Body.Close()
			if v.Hostname != "" {
				m.setURL("https://" + v.Hostname)
				m.setState("running", "")
			}
		}
		time.Sleep(5 * time.Second)
	}
}

func (m *TunnelManager) localBinary() string {
	return filepath.Join(m.store.DataDir(), "bin", "cloudflared")
}

func (m *TunnelManager) findBinary() (string, bool) {
	if p, err := exec.LookPath("cloudflared"); err == nil {
		return p, true
	}
	if _, err := os.Stat(m.localBinary()); err == nil {
		return m.localBinary(), true
	}
	return "", false
}

// ensureBinary returns cloudflared, downloading the official release into the
// data dir when it is not installed.
func (m *TunnelManager) ensureBinary(ctx context.Context) (string, error) {
	if p, ok := m.findBinary(); ok {
		return p, nil
	}
	if runtime.GOOS != "linux" {
		return "", errors.New("请先安装 cloudflared (macOS: brew install cloudflared)")
	}
	arch := map[string]string{"amd64": "amd64", "arm64": "arm64", "arm": "arm", "386": "386"}[runtime.GOARCH]
	if arch == "" {
		return "", errors.New("不支持的架构 " + runtime.GOARCH)
	}
	m.setState("downloading", "")
	url := "https://github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-linux-" + arch
	m.logLine("下载 " + url)
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	resp, err := (&http.Client{Timeout: 5 * time.Minute}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	dst := m.localBinary()
	os.MkdirAll(filepath.Dir(dst), 0o755)
	tmp := dst + ".part"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return "", err
	}
	n, err := io.Copy(f, resp.Body)
	f.Close()
	if err != nil || n < 1<<20 {
		os.Remove(tmp)
		return "", fmt.Errorf("下载不完整 (%d 字节)", n)
	}
	if err := os.Rename(tmp, dst); err != nil {
		return "", err
	}
	m.logLine(fmt.Sprintf("cloudflared 已下载 (%.1f MB)", float64(n)/(1<<20)))
	return dst, nil
}

// ---- automatic named-tunnel setup through the Cloudflare API ----

const cfAPI = "https://api.cloudflare.com/client/v4"

type cfClient struct {
	token string
	http  *http.Client
}

type cfResp struct {
	Success bool            `json:"success"`
	Errors  []cfErr         `json:"errors"`
	Result  json.RawMessage `json:"result"`
}

type cfErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (c *cfClient) do(method, path string, body any, out any) error {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, cfAPI+path, rd)
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var r cfResp
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return fmt.Errorf("Cloudflare 返回 HTTP %d", resp.StatusCode)
	}
	if !r.Success {
		var msgs []string
		for _, e := range r.Errors {
			msgs = append(msgs, fmt.Sprintf("%d %s", e.Code, e.Message))
		}
		return errors.New("Cloudflare: " + strings.Join(msgs, "; "))
	}
	if out != nil {
		return json.Unmarshal(r.Result, out)
	}
	return nil
}

// AutoSetup creates (or reuses) a remotely-managed tunnel named
// hxoa-<hostname>, routes hostname to the agent API, points DNS at it, and
// switches hxoad to run it. The API token is used once and not stored.
// Token permissions: Account › Cloudflare Tunnel › Edit, Zone › DNS › Edit, Zone › Zone › Read.
func (m *TunnelManager) AutoSetup(apiToken, hostname string) error {
	hostname = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(hostname, "https://"), "http://")))
	hostname = strings.TrimRight(hostname, "/")
	if apiToken == "" || !strings.Contains(hostname, ".") {
		return errors.New("需要 API 令牌和完整域名, 如 oa.example.com")
	}
	c := &cfClient{token: strings.TrimSpace(apiToken), http: &http.Client{Timeout: 30 * time.Second}}

	// 1. zone: try the hostname's parents until one is a zone in this account
	type zone struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Account struct {
			ID string `json:"id"`
		} `json:"account"`
	}
	var z *zone
	labels := strings.Split(hostname, ".")
	for i := 0; i+1 < len(labels) && z == nil; i++ {
		var zs []zone
		if err := c.do("GET", "/zones?name="+strings.Join(labels[i:], "."), nil, &zs); err != nil {
			return err
		}
		if len(zs) > 0 {
			z = &zs[0]
		}
	}
	if z == nil {
		return errors.New("这个令牌看不到 " + hostname + " 所在的域名 (需要 Zone:Read 权限, 且域名托管在 Cloudflare)")
	}
	acc := z.Account.ID
	m.logLine("域名 " + z.Name + " / 账号 " + acc)

	// 2. tunnel: reuse by name or create
	name := "hxoa-" + strings.ReplaceAll(hostname, ".", "-")
	type tunnel struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	var ts []tunnel
	if err := c.do("GET", "/accounts/"+acc+"/cfd_tunnel?is_deleted=false&name="+name, nil, &ts); err != nil {
		return err
	}
	var tun tunnel
	if len(ts) > 0 {
		tun = ts[0]
		m.logLine("复用隧道 " + tun.Name)
	} else {
		if err := c.do("POST", "/accounts/"+acc+"/cfd_tunnel", map[string]any{"name": name, "config_src": "cloudflare"}, &tun); err != nil {
			return err
		}
		m.logLine("已创建隧道 " + tun.Name)
	}

	// 3. ingress: hostname -> local agent API
	ingress := map[string]any{"config": map[string]any{"ingress": []map[string]any{
		{"hostname": hostname, "service": m.target},
		{"service": "http_status:404"},
	}}}
	if err := c.do("PUT", "/accounts/"+acc+"/cfd_tunnel/"+tun.ID+"/configurations", ingress, nil); err != nil {
		return err
	}

	// 4. DNS CNAME, created or updated
	target := tun.ID + ".cfargotunnel.com"
	type rec struct {
		ID      string `json:"id"`
		Type    string `json:"type"`
		Content string `json:"content"`
	}
	var recs []rec
	if err := c.do("GET", "/zones/"+z.ID+"/dns_records?name="+hostname, nil, &recs); err != nil {
		return err
	}
	body := map[string]any{"type": "CNAME", "name": hostname, "content": target, "proxied": true, "comment": "hxoa"}
	if len(recs) > 0 {
		r := recs[0]
		if r.Type != "CNAME" || !strings.HasSuffix(r.Content, ".cfargotunnel.com") {
			return errors.New(hostname + " 已有一条 " + r.Type + " 记录指向 " + r.Content + ", 为了不破坏它, 请换个子域名或先手动删除")
		}
		if err := c.do("PUT", "/zones/"+z.ID+"/dns_records/"+r.ID, body, nil); err != nil {
			return err
		}
	} else if err := c.do("POST", "/zones/"+z.ID+"/dns_records", body, nil); err != nil {
		return err
	}
	m.logLine("DNS " + hostname + " -> " + target)

	// 5. run token
	var tok string
	if err := c.do("GET", "/accounts/"+acc+"/cfd_tunnel/"+tun.ID+"/token", nil, &tok); err != nil {
		return err
	}
	m.store.SetTunnel(TunnelConfig{Mode: "token", Token: tok, Hostname: hostname, TunnelID: tun.ID})
	m.Start()
	return nil
}

// tokenHostname is best effort: a pasted tunnel token does not say which
// hostname routes to it, so the user types it.
func (m *TunnelManager) UseToken(token, hostname string) error {
	token = strings.TrimSpace(token)
	if strings.HasPrefix(token, "cloudflared ") || strings.Contains(token, "--token") {
		if i := strings.LastIndex(token, " "); i >= 0 {
			token = token[i+1:]
		}
	}
	hostname = strings.TrimRight(strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(hostname), "https://"), "http://"), "/")
	if len(token) < 40 || hostname == "" {
		return errors.New("需要隧道 token 和它对应的域名")
	}
	m.store.SetTunnel(TunnelConfig{Mode: "token", Token: token, Hostname: hostname})
	m.Start()
	return nil
}

func (m *TunnelManager) SetMode(mode string) error {
	switch mode {
	case "off", "quick":
	case "token":
		if m.store.Tunnel().Token == "" {
			return errors.New("还没有隧道 token")
		}
	default:
		return errors.New("未知模式 " + mode)
	}
	cfg := m.store.Tunnel()
	cfg.Mode = mode
	m.store.SetTunnel(cfg)
	m.Start()
	return nil
}

// scrubEnv drops hxoad's own secrets from a child's environment.
func scrubEnv(env []string) []string {
	out := env[:0:0]
	for _, e := range env {
		k, _, _ := strings.Cut(e, "=")
		if strings.HasPrefix(k, "HXOA_") || strings.HasPrefix(k, "CF_") || k == "TUNNEL_TOKEN" {
			continue
		}
		out = append(out, e)
	}
	return out
}
