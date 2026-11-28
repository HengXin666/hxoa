// Command hxoad is the local server: it shares chosen directories with remote
// agents through a Cloudflare tunnel it manages itself, and serves an admin
// panel for choosing directories, issuing tokens and configuring the tunnel.
package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/HengXin666/hxoa/internal/server"
	"github.com/HengXin666/hxoa/web"
)

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func main() {
	data := flag.String("data", env("HXOA_DATA", defaultData()), "state directory")
	agentAddr := flag.String("agent", env("HXOA_AGENT_ADDR", "127.0.0.1:7070"), "agent API listen address (tunnel target)")
	adminAddr := flag.String("admin", env("HXOA_ADMIN_ADDR", "127.0.0.1:7071"), "panel listen address")
	adminPw := flag.String("admin-password", env("HXOA_ADMIN_PASSWORD", ""), "initial panel password for user admin, used until an account is saved in the panel")
	panelAuth := flag.String("panel-auth", env("HXOA_PANEL_AUTH", "password"), "panel auth: password (own login) | none (trust the reverse proxy in front, e.g. SafeLine 身份认证)")
	public := flag.String("public-url", env("HXOA_PUBLIC_URL", ""), "fixed public URL, overrides the managed tunnel")
	metrics := flag.String("tunnel-metrics", env("HXOA_TUNNEL_METRICS", ""), "external cloudflared metrics URL (docker sidecar)")
	quick := flag.Bool("quick-tunnel", false, "switch the managed tunnel to quick mode on start")
	version := flag.Bool("version", false, "print version")
	flag.Parse()
	if *version {
		fmt.Println(server.ServerVersion)
		return
	}

	store, err := server.OpenStore(*data)
	if err != nil {
		log.Fatal(err)
	}
	host, _, _ := net.SplitHostPort(*adminAddr)
	auth, err := server.NewAuth(*data, *panelAuth, *adminPw, isLoopback(host))
	if err != nil {
		log.Fatal(err)
	}
	if !isLoopback(host) && auth.Mode == server.AuthPassword && !auth.HasAccount() {
		log.Fatal("面板监听在 " + *adminAddr + " (不只是本机), 必须设置 -admin-password 或 HXOA_ADMIN_PASSWORD, 或先在本机面板里设置账号")
	}
	if auth.Mode == server.AuthNone {
		log.Printf("面板认证: none, 由上游代理负责; 确保 %s 只能经由它访问", *adminAddr)
	}
	tun := server.NewTunnelManager(store, *agentAddr, *public, *metrics)
	if *quick && *metrics == "" {
		tun.SetMode("quick")
	} else if *metrics == "" {
		tun.Start()
	}
	srv := &server.Server{Store: store, Tunnel: tun, Auth: auth}
	memDir := env("HXOA_MEMORY", *data+"/memory")
	if mem, err := server.OpenMemory(memDir, os.Getenv("HXOA_MEMORY_REMOTE")); err != nil {
		log.Printf("协作记忆不可用: %v", err)
	} else {
		srv.Memory = mem
		log.Printf("协作记忆 %s", memDir)
	}

	go func() {
		log.Printf("agent API on %s", *agentAddr)
		s := &http.Server{Addr: *agentAddr, Handler: srv.AgentHandler(), ReadHeaderTimeout: 10 * time.Second}
		log.Fatal(s.ListenAndServe())
	}()
	log.Printf("panel on http://%s", *adminAddr)
	s := &http.Server{Addr: *adminAddr, Handler: srv.AdminHandler(web.FS()), ReadHeaderTimeout: 10 * time.Second}
	log.Fatal(s.ListenAndServe())
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func defaultData() string {
	if d, err := os.UserConfigDir(); err == nil {
		return d + "/hxoad"
	}
	return ".hxoad"
}
