package server

import (
	"crypto/subtle"
	"encoding/json"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// AdminHandler serves the local panel and its API. It must only be reachable
// from this machine: it is bound to 127.0.0.1 and never sent through the tunnel.
func (s *Server) AdminHandler(static fs.FS) http.Handler {
	mux := http.NewServeMux()
	fail := func(w http.ResponseWriter, err error) {
		writeJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
	}
	mux.HandleFunc("GET /api/tunnel", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"ok": true, "tunnel": s.Tunnel.Status()})
	})
	mux.HandleFunc("POST /api/tunnel/mode", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Mode string }
		json.NewDecoder(r.Body).Decode(&in)
		if err := s.Tunnel.SetMode(in.Mode); err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true})
	})
	mux.HandleFunc("POST /api/tunnel/token", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Token, Hostname string }
		json.NewDecoder(r.Body).Decode(&in)
		if err := s.Tunnel.UseToken(in.Token, in.Hostname); err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true})
	})
	mux.HandleFunc("POST /api/tunnel/auto", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			APIToken string `json:"api_token"`
			Hostname string `json:"hostname"`
		}
		json.NewDecoder(r.Body).Decode(&in)
		if err := s.Tunnel.AutoSetup(in.APIToken, in.Hostname); err != nil {
			s.Tunnel.logLine("自动配置失败: " + err.Error())
			fail(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true})
	})
	mux.Handle("/", http.FileServer(http.FS(static)))

	mux.HandleFunc("GET /api/state", func(w http.ResponseWriter, r *http.Request) {
		ws, ts := s.Store.Snapshot()
		type tokView struct {
			Token
			Online bool          `json:"online"`
			Worker *WorkerStatus `json:"worker,omitempty"`
		}
		type wsView struct {
			Workspace
			Git    bool      `json:"git"`
			Branch string    `json:"branch,omitempty"`
			Online int       `json:"online"`
			Tokens []tokView `json:"tokens"`
		}
		out := []wsView{}
		for _, w := range ws {
			v := wsView{Workspace: w, Git: isGitRepo(w.Path), Tokens: []tokView{}}
			if v.Git {
				v.Branch = w.Branch()
			}
			for _, t := range ts {
				if t.Workspace != w.ID {
					continue
				}
				tv := tokView{Token: t, Online: s.Store.Online(&t)}
				if w.Workers {
					st := workerStatus(&w, &t)
					tv.Worker = &st
				}
				if tv.Online {
					v.Online++
				}
				v.Tokens = append(v.Tokens, tv)
			}
			out = append(out, v)
		}
		writeJSON(w, 200, map[string]any{"ok": true, "public_url": s.PublicURL(), "tunnel": s.Tunnel.Status(), "workspaces": out, "server": ServerVersion})
	})

	mux.HandleFunc("GET /api/ops", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"ok": true, "ops": s.ops.list()})
	})

	mux.HandleFunc("POST /api/workspaces", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Name    string `json:"name"`
			Path    string `json:"path"`
			Workers bool   `json:"workers"`
			Exec    *bool  `json:"exec"`
		}
		json.NewDecoder(r.Body).Decode(&in)
		ex := true
		if in.Exec != nil {
			ex = *in.Exec
		}
		ws, err := s.Store.AddWorkspace(in.Name, expandHome(in.Path), in.Workers, ex)
		if err != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "workspace": ws})
	})

	mux.HandleFunc("PATCH /api/workspaces/{id}", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Name    *string `json:"name"`
			Workers *bool   `json:"workers"`
			Exec    *bool   `json:"exec"`
		}
		json.NewDecoder(r.Body).Decode(&in)
		ws, err := s.Store.UpdateWorkspace(r.PathValue("id"), in.Name, in.Workers, in.Exec)
		if err != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "workspace": ws})
	})

	mux.HandleFunc("DELETE /api/workspaces/{id}", func(w http.ResponseWriter, r *http.Request) {
		s.Store.DeleteWorkspace(r.PathValue("id"))
		writeJSON(w, 200, map[string]any{"ok": true})
	})

	mux.HandleFunc("POST /api/workspaces/{id}/tokens", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Label string `json:"label"`
		}
		json.NewDecoder(r.Body).Decode(&in)
		t, err := s.Store.AddToken(r.PathValue("id"), strings.TrimSpace(in.Label))
		if err != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		ws := s.Store.Workspace(t.Workspace)
		writeJSON(w, 200, map[string]any{"ok": true, "token": t, "prompt": s.Prompt(ws, t)})
	})

	mux.HandleFunc("GET /api/tokens/{id}/prompt", func(w http.ResponseWriter, r *http.Request) {
		t, ws := s.Store.TokenByID(r.PathValue("id"))
		if t == nil || ws == nil {
			writeJSON(w, 404, map[string]any{"ok": false, "error": "令牌不存在"})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "prompt": s.Prompt(ws, t)})
	})

	mux.HandleFunc("DELETE /api/tokens/{id}", func(w http.ResponseWriter, r *http.Request) {
		t, ws := s.Store.TokenByID(r.PathValue("id"))
		s.Store.DeleteToken(r.PathValue("id"))
		if t != nil {
			killJobs(t.ID)
		}
		// Deleting a token cleans up its worker: the worktree goes when it has
		// nothing uncommitted, the branch when it is fully merged. Unmerged
		// work is kept for the user to deal with.
		if t != nil && ws != nil && ws.Workers {
			if st := workerStatus(ws, t); st.Exists && len(st.Dirty) == 0 {
				removeWorker(ws, t)
			}
		}
		writeJSON(w, 200, map[string]any{"ok": true})
	})

	mux.HandleFunc("POST /api/tokens/{id}/merge", func(w http.ResponseWriter, r *http.Request) {
		t, ws := s.Store.TokenByID(r.PathValue("id"))
		if t == nil || ws == nil || !ws.Workers {
			writeJSON(w, 400, map[string]any{"ok": false, "error": "不是 worker 连接"})
			return
		}
		out, err := mergeWorker(ws, t)
		if err != nil {
			writeJSON(w, 409, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		s.ops.add(opEntry{At: time.Now(), Who: "你", WS: ws.Name, Op: "merge", Target: t.Label, OK: true})
		writeJSON(w, 200, map[string]any{"ok": true, "output": out})
	})

	// Directory picker for choosing a workspace location.
	mux.HandleFunc("GET /api/browse", func(w http.ResponseWriter, r *http.Request) {
		p := expandHome(r.URL.Query().Get("path"))
		if p == "" {
			p = homeDir()
		}
		p = filepath.Clean(p)
		ents, err := os.ReadDir(p)
		if err != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		dirs := []string{}
		for _, e := range ents {
			if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
				dirs = append(dirs, e.Name())
			}
		}
		sort.Strings(dirs)
		_, gerr := os.Stat(filepath.Join(p, ".git"))
		writeJSON(w, 200, map[string]any{"ok": true, "path": p, "parent": filepath.Dir(p), "dirs": dirs, "git": gerr == nil})
	})
	return s.guard(mux)
}

// guard asks for the panel password (HTTP Basic, any user name) when one is
// set, and refuses cross-site writes so another tab cannot drive the panel.
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.AdminPassword != "" {
			_, pw, ok := r.BasicAuth()
			if !ok || subtle.ConstantTimeCompare([]byte(pw), []byte(s.AdminPassword)) != 1 {
				w.Header().Set("WWW-Authenticate", `Basic realm="hxoa"`)
				http.Error(w, "需要面板密码", http.StatusUnauthorized)
				return
			}
		}
		if r.Method != "GET" {
			if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
				http.Error(w, "cross-site request refused", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func homeDir() string {
	if h := os.Getenv("HXOA_HOME"); h != "" {
		return h
	}
	h, _ := os.UserHomeDir()
	return h
}

func expandHome(p string) string {
	p = strings.TrimSpace(p)
	if p == "~" {
		return homeDir()
	}
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(homeDir(), p[2:])
	}
	return p
}

// Prompt is the text the user pastes to an agent. Kept as short as the
// user's own hand-written prompt: the client prints the rest (mode, branch,
// exec on/off) on login and carries the full rules in `hxoa guide`.
func (s *Server) Prompt(ws *Workspace, t *Token) string {
	base := s.PublicURL()
	if base == "" {
		base = "<隧道地址还没就绪>"
	}
	p := "因为本地工作区纯源码就非常大, 无法打包或者让你clone, 而且有未提交的修改, 你必须下载 https://github.com/HengXin666/hxoa 连接我本地工作区, 才能进行后续工作:\n\n" +
		"hxoa --base " + base + " --login " + t.Secret + "\n" +
		"装完直接 hxoa ls 就能看到我的文件, 连的是本机这一个工作区"
	if ws.Workers {
		p += "\n\n开了并行编辑: 你在自己的 git 工作树 (分支 " + workerBranch(t) + ") 里改, 做完一段就 commit 并告诉我, 我在面板上合并。"
	}
	return p
}
