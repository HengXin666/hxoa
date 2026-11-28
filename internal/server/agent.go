package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/HengXin666/hxoa/internal/proto"
)

const ServerVersion = "hxoad/2.5.3"

type Server struct {
	Store  *Store
	Tunnel *TunnelManager
	Auth   *Auth // panel login
	ops    opLog
	remote remoteLimiter
}

func (s *Server) PublicURL() string { return s.Tunnel.URL() }

type opEntry struct {
	At     time.Time `json:"at"`
	Who    string    `json:"who"`
	WS     string    `json:"ws"`
	Op     string    `json:"op"`
	Target string    `json:"target"`
	OK     bool      `json:"ok"`
	Note   string    `json:"note,omitempty"`
}

type opLog struct {
	mu   sync.Mutex
	ring []opEntry
}

func (l *opLog) add(e opEntry) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.ring = append(l.ring, e)
	if len(l.ring) > 300 {
		l.ring = l.ring[len(l.ring)-300:]
	}
}

func (l *opLog) list() []opEntry {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]opEntry, len(l.ring))
	for i := range l.ring {
		out[i] = l.ring[len(l.ring)-1-i]
	}
	return out
}

type reqCtx struct {
	tok    *Token
	ws     *Workspace
	root   string
	branch string
	base   string
	secret string
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.Encode(v)
}

func statusFor(code string) int {
	switch code {
	case proto.CodeConflict, proto.CodeExists, proto.CodeAmbiguous, proto.CodeNoMatch:
		return http.StatusConflict
	case proto.CodeNotFound:
		return http.StatusNotFound
	case proto.CodeDenied:
		return http.StatusForbidden
	case proto.CodeAuth:
		return http.StatusUnauthorized
	case proto.CodeTooLarge:
		return http.StatusRequestEntityTooLarge
	}
	return http.StatusBadRequest
}

func writeErr(w http.ResponseWriter, err error) {
	var ae *apiErr
	if errors.As(err, &ae) {
		writeJSON(w, statusFor(ae.code), proto.Error{Error: ae.msg, Code: ae.code, Current: ae.current})
		return
	}
	writeJSON(w, http.StatusBadRequest, proto.Error{Error: err.Error(), Code: proto.CodeBadRequest})
}

func bearer(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimSpace(h[7:])
	}
	return r.Header.Get("X-Hxoa-Token")
}

// AgentHandler is the API exposed through the tunnel.
func (s *Server) AgentHandler() http.Handler {
	mux := http.NewServeMux()
	h := func(path, op string, fn func(w http.ResponseWriter, r *http.Request, c *reqCtx) (string, error)) {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			secret := bearer(r)
			tok, ws := s.Store.Auth(secret, clientIP(r))
			if tok == nil {
				writeJSON(w, 401, proto.Error{Error: "令牌无效或已被删除", Code: proto.CodeAuth})
				return
			}
			c := &reqCtx{tok: tok, ws: ws, secret: secret}
			root, branch, base, err := ensureWorker(ws, tok)
			if err != nil {
				writeErr(w, err)
				return
			}
			c.root, c.branch, c.base = root, branch, base
			r.Body = http.MaxBytesReader(w, r.Body, 64<<20)
			target, err := fn(w, r, c)
			e := opEntry{At: time.Now(), Who: tok.Label, WS: ws.Name, Op: op, Target: target, OK: err == nil}
			if err != nil {
				e.Note = err.Error()
				writeErr(w, err)
			}
			if op != "job" || err != nil {
				s.ops.add(e)
			}
		})
	}
	decode := func(r *http.Request, v any) error {
		if err := json.NewDecoder(r.Body).Decode(v); err != nil {
			return errf(proto.CodeBadRequest, "请求体不是合法 JSON: "+err.Error())
		}
		return nil
	}

	h("/v1/hello", "hello", func(w http.ResponseWriter, r *http.Request, c *reqCtx) (string, error) {
		mode := "direct"
		if c.ws.Workers {
			mode = "worker"
		}
		writeJSON(w, 200, proto.Hello{OK: true, Server: ServerVersion, Proto: proto.Version, Workspace: c.ws.Name,
			Identity: c.tok.Label, Root: c.root, Mode: mode, Branch: c.branch, Base: c.base, Exec: c.ws.Exec, Task: c.tok.Task.wire()})
		return "", nil
	})
	h("/v1/task", "task", func(w http.ResponseWriter, r *http.Request, c *reqCtx) (string, error) {
		if r.Method == http.MethodGet {
			writeJSON(w, 200, map[string]any{"ok": true, "task": c.tok.Task.wire()})
			return "", nil
		}
		var req proto.TaskReq
		if err := decode(r, &req); err != nil {
			return "", err
		}
		t, err := cleanTask(req.Issues, req.Note, "agent")
		if err != nil {
			return "", errf(proto.CodeBadRequest, err.Error())
		}
		s.Store.SetTask(c.tok.ID, t)
		writeJSON(w, 200, map[string]any{"ok": true, "task": t.wire()})
		return taskText(t), nil
	})
	// Agent 自己把 worker 分支合进用户当前分支: 与面板的合并相同, 有冲突或用户目录有未提交修改就撤销并报错
	h("/v1/merge", "merge", func(w http.ResponseWriter, r *http.Request, c *reqCtx) (string, error) {
		if r.Method != http.MethodPost {
			return "", errf(proto.CodeBadRequest, "用 POST")
		}
		if !c.ws.Workers {
			return "", errf(proto.CodeDenied, "direct 模式直接改用户的目录, 不用合并")
		}
		out, err := mergeWorker(c.ws, c.tok)
		if err != nil {
			return "", errf(proto.CodeBadRequest, err.Error())
		}
		writeJSON(w, 200, map[string]any{"ok": true, "base": c.ws.Branch(), "output": out})
		return c.ws.Branch(), nil
	})
	h("/v1/bye", "bye", func(w http.ResponseWriter, r *http.Request, c *reqCtx) (string, error) {
		s.Store.Bye(c.secret)
		writeJSON(w, 200, map[string]any{"ok": true})
		return "", nil
	})
	h("/v1/ls", "ls", func(w http.ResponseWriter, r *http.Request, c *reqCtx) (string, error) {
		q := r.URL.Query()
		depth, _ := strconv.Atoi(q.Get("depth"))
		max, _ := strconv.Atoi(q.Get("max"))
		out, err := doLs(c.root, q.Get("path"), depth, max)
		if err != nil {
			return q.Get("path"), err
		}
		writeJSON(w, 200, out)
		return q.Get("path"), nil
	})
	h("/v1/read", "read", func(w http.ResponseWriter, r *http.Request, c *reqCtx) (string, error) {
		q := r.URL.Query()
		start, _ := strconv.Atoi(q.Get("start"))
		end, _ := strconv.Atoi(q.Get("end"))
		out, err := doRead(c.root, q.Get("path"), start, end)
		if err != nil {
			return q.Get("path"), err
		}
		writeJSON(w, 200, out)
		return q.Get("path"), nil
	})
	h("/v1/write", "write", func(w http.ResponseWriter, r *http.Request, c *reqCtx) (string, error) {
		var req proto.WriteReq
		if err := decode(r, &req); err != nil {
			return "", err
		}
		out, err := doWrite(c.root, &req)
		if err != nil {
			return req.Path, err
		}
		writeJSON(w, 200, out)
		return req.Path, nil
	})
	h("/v1/patch", "patch", func(w http.ResponseWriter, r *http.Request, c *reqCtx) (string, error) {
		var req proto.PatchReq
		if err := decode(r, &req); err != nil {
			return "", err
		}
		out, err := doPatch(c.root, &req)
		if err != nil {
			return req.Path, err
		}
		writeJSON(w, 200, out)
		return req.Path, nil
	})
	h("/v1/rm", "rm", func(w http.ResponseWriter, r *http.Request, c *reqCtx) (string, error) {
		var req proto.RmReq
		if err := decode(r, &req); err != nil {
			return "", err
		}
		if err := doRm(c.root, &req); err != nil {
			return req.Path, err
		}
		writeJSON(w, 200, map[string]any{"ok": true})
		return req.Path, nil
	})
	h("/v1/mv", "mv", func(w http.ResponseWriter, r *http.Request, c *reqCtx) (string, error) {
		var req proto.MvReq
		if err := decode(r, &req); err != nil {
			return "", err
		}
		if err := doMv(c.root, &req); err != nil {
			return req.From, err
		}
		writeJSON(w, 200, map[string]any{"ok": true})
		return req.From + " -> " + req.To, nil
	})
	h("/v1/mkdir", "mkdir", func(w http.ResponseWriter, r *http.Request, c *reqCtx) (string, error) {
		var req proto.MkdirReq
		if err := decode(r, &req); err != nil {
			return "", err
		}
		created, err := doMkdir(c.root, &req)
		if err != nil {
			return req.Path, err
		}
		writeJSON(w, 200, map[string]any{"ok": true, "created": created})
		return req.Path, nil
	})
	h("/v1/grep", "grep", func(w http.ResponseWriter, r *http.Request, c *reqCtx) (string, error) {
		var req proto.GrepReq
		if err := decode(r, &req); err != nil {
			return "", err
		}
		out, err := doGrep(c.root, &req)
		if err != nil {
			return req.Pattern, err
		}
		writeJSON(w, 200, out)
		return req.Pattern, nil
	})
	h("/v1/exec", "exec", func(w http.ResponseWriter, r *http.Request, c *reqCtx) (string, error) {
		if !c.ws.Exec {
			return "", errf(proto.CodeDenied, "这个工作区没有开启命令执行")
		}
		var req proto.ExecReq
		if err := decode(r, &req); err != nil {
			return "", err
		}
		out, err := doExec(c.root, c.tok.ID, &req)
		if err != nil {
			return req.Cmd, err
		}
		writeJSON(w, 200, out)
		return req.Cmd, nil
	})
	h("/v1/job", "job", func(w http.ResponseWriter, r *http.Request, c *reqCtx) (string, error) {
		q := r.URL.Query()
		wait, _ := strconv.Atoi(q.Get("wait"))
		out, err := jobStatus(c.tok.ID, q.Get("id"), wait, q.Get("kill") == "1")
		if err != nil {
			return q.Get("id"), err
		}
		writeJSON(w, 200, out)
		return q.Get("id"), nil
	})
	h("/v1/tar", "tar", func(w http.ResponseWriter, r *http.Request, c *reqCtx) (string, error) {
		q := r.URL.Query()
		if _, err := resolve(c.root, q.Get("path"), false); err != nil {
			return q.Get("path"), err
		}
		w.Header().Set("Content-Type", "application/gzip")
		return q.Get("path"), writeTar(c.root, q.Get("path"), w, q.Get("all") != "1")
	})
	admin := s.RemoteAdminHandler()
	mux.Handle(adminPrefix, admin)
	mux.Handle(adminPrefixOld, admin)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 404, proto.Error{Error: "hxoad agent API; 用 hxoa 客户端连接", Code: proto.CodeNotFound})
	})
	return mux
}
