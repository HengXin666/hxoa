package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/HengXin666/hxoa/internal/proto"
)

// Remote admin: the panel API reachable through the tunnel at /admin/v1/*
// with an admin token, so another app (e.g. HX-AIOA on a phone) can manage
// workspaces and connection tokens. Off until the user mints a token in the
// panel; only the hash is stored. Minting admin tokens and tunnel / account
// settings stay local-only.

const (
	adminPrefix     = "/admin/v1/"
	adminSecretPre  = "hxadm_"
	remoteFailLimit = 5
	remoteLockStep  = time.Minute
)

// AdminToken is a remote admin credential. Secret is shown once on creation.
type AdminToken struct {
	ID       string    `json:"id"`
	Label    string    `json:"label"`
	Hash     string    `json:"hash"`
	Hint     string    `json:"hint"` // first characters, to tell tokens apart
	Created  time.Time `json:"created"`
	LastSeen time.Time `json:"last_seen,omitempty"`
	LastIP   string    `json:"last_ip,omitempty"`
	Ops      int64     `json:"ops"`
}

// adminTokenView never carries the hash.
type adminTokenView struct {
	ID       string    `json:"id"`
	Label    string    `json:"label"`
	Hint     string    `json:"hint"`
	Created  time.Time `json:"created"`
	LastSeen time.Time `json:"last_seen,omitempty"`
	LastIP   string    `json:"last_ip,omitempty"`
	Ops      int64     `json:"ops"`
}

func hashSecret(secret string) string {
	h := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(h[:])
}

func (s *Store) AddAdminToken(label string) (string, *AdminToken, error) {
	if !labelRe.MatchString(label) {
		return "", nil, errors.New("名字只能用字母数字 . _ -, 最长 32")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, a := range s.admins {
		if a.Label == label {
			return "", nil, errors.New("已经有叫 " + label + " 的管理令牌")
		}
	}
	secret := adminSecretPre + randHex(24)
	a := &AdminToken{ID: randHex(4), Label: label, Hash: hashSecret(secret), Hint: secret[:len(adminSecretPre)+4], Created: time.Now()}
	s.admins[a.Hash] = a
	s.dirty = true
	s.saveLocked()
	c := *a
	return secret, &c, nil
}

func (s *Store) DeleteAdminToken(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, a := range s.admins {
		if a.ID == id {
			delete(s.admins, k)
			s.dirty = true
			s.saveLocked()
			return true
		}
	}
	return false
}

func (s *Store) AdminTokens() []adminTokenView {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []adminTokenView{}
	for _, a := range s.admins {
		out = append(out, adminTokenView{ID: a.ID, Label: a.Label, Hint: a.Hint, Created: a.Created, LastSeen: a.LastSeen, LastIP: a.LastIP, Ops: a.Ops})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created.Before(out[j].Created) })
	return out
}

// AuthAdmin resolves an admin secret and records the visit.
func (s *Store) AuthAdmin(secret, ip string) *AdminToken {
	if !strings.HasPrefix(secret, adminSecretPre) {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	a := s.admins[hashSecret(secret)]
	if a == nil {
		return nil
	}
	a.LastSeen = time.Now()
	a.LastIP = ip
	a.Ops++
	s.dirty = true
	c := *a
	return &c
}

func (s *Store) HasAdminTokens() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.admins) > 0
}

func (s *Server) adminTokenRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/admin-tokens", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"ok": true, "tokens": s.Store.AdminTokens(), "base": s.PublicURL()})
	})
	mux.HandleFunc("POST /api/admin-tokens", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Label string `json:"label"`
		}
		json.NewDecoder(r.Body).Decode(&in)
		secret, a, err := s.Store.AddAdminToken(strings.TrimSpace(in.Label))
		if err != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		s.ops.add(opEntry{At: time.Now(), Who: "你", Op: "admin-token", Target: a.Label, OK: true})
		writeJSON(w, 200, map[string]any{"ok": true, "id": a.ID, "label": a.Label, "secret": secret, "base": s.PublicURL()})
	})
	mux.HandleFunc("DELETE /api/admin-tokens/{id}", func(w http.ResponseWriter, r *http.Request) {
		ok := s.Store.DeleteAdminToken(r.PathValue("id"))
		writeJSON(w, 200, map[string]any{"ok": ok})
	})
}

// What an admin token may reach: workspace and connection-token management,
// the directory picker, the op log and read-only tunnel status. Not reachable:
// admin tokens themselves, tunnel settings, panel account.
var remoteAllowed = []struct {
	method string
	re     *regexp.Regexp
}{
	{"GET", regexp.MustCompile(`^state$`)},
	{"GET", regexp.MustCompile(`^ops$`)},
	{"GET", regexp.MustCompile(`^tunnel$`)},
	{"GET", regexp.MustCompile(`^browse$`)},
	{"POST", regexp.MustCompile(`^mkdir$`)},
	{"POST", regexp.MustCompile(`^workspaces$`)},
	{"PATCH", regexp.MustCompile(`^workspaces/[0-9a-f]+$`)},
	{"DELETE", regexp.MustCompile(`^workspaces/[0-9a-f]+$`)},
	{"POST", regexp.MustCompile(`^workspaces/[0-9a-f]+/tokens$`)},
	{"GET", regexp.MustCompile(`^tokens/[0-9a-f]+/prompt$`)},
	{"DELETE", regexp.MustCompile(`^tokens/[0-9a-f]+$`)},
	{"POST", regexp.MustCompile(`^tokens/[0-9a-f]+/merge$`)},
}

func remoteAllow(method, rest string) bool {
	for _, a := range remoteAllowed {
		if a.method == method && a.re.MatchString(rest) {
			return true
		}
	}
	return false
}

type actorKey struct{}

// actor names who did a panel operation, for the op log.
func actor(r *http.Request) string {
	if v, ok := r.Context().Value(actorKey{}).(string); ok {
		return v
	}
	return "你"
}

type remoteLimiter struct {
	mu    sync.Mutex
	fails map[string]*failCount
}

func (l *remoteLimiter) blocked(ip string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	if f := l.fails[ip]; f != nil && time.Now().Before(f.until) {
		return time.Until(f.until)
	}
	return 0
}

func (l *remoteLimiter) fail(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.fails == nil {
		l.fails = map[string]*failCount{}
	}
	if len(l.fails) > 1024 {
		cut := time.Now().Add(-24 * time.Hour)
		for k, f := range l.fails {
			if f.last.Before(cut) && time.Now().After(f.until) {
				delete(l.fails, k)
			}
		}
	}
	f := l.fails[ip]
	if f == nil {
		f = &failCount{}
		l.fails[ip] = f
	}
	f.n++
	f.last = time.Now()
	if f.n >= remoteFailLimit {
		f.until = time.Now().Add(time.Duration(f.n-remoteFailLimit+1) * remoteLockStep)
	}
}

func (l *remoteLimiter) ok(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.fails, ip)
}

// RemoteAdminHandler serves /admin/v1/* on the agent listener.
func (s *Server) RemoteAdminHandler() http.Handler {
	api := http.NewServeMux()
	s.apiRoutes(api)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r)
		if d := s.remote.blocked(ip); d > 0 {
			writeJSON(w, 429, proto.Error{Error: "管理令牌错误次数过多, 请 " + strconv.Itoa(int(d.Seconds())+1) + " 秒后再试", Code: proto.CodeAuth})
			return
		}
		a := s.Store.AuthAdmin(bearer(r), ip)
		if a == nil {
			s.remote.fail(ip)
			s.ops.add(opEntry{At: time.Now(), Who: ip, Op: "admin", Target: r.URL.Path, OK: false, Note: "管理令牌无效"})
			writeJSON(w, 401, proto.Error{Error: "管理令牌无效或已被删除", Code: proto.CodeAuth})
			return
		}
		s.remote.ok(ip)
		rest := strings.TrimPrefix(r.URL.Path, adminPrefix)
		who := "管理:" + a.Label
		if rest == "hello" && r.Method == "GET" {
			writeJSON(w, 200, map[string]any{"ok": true, "server": ServerVersion, "admin": a.Label, "public_url": s.PublicURL()})
			return
		}
		if !remoteAllow(r.Method, rest) {
			s.ops.add(opEntry{At: time.Now(), Who: who, Op: "admin", Target: r.Method + " " + rest, OK: false, Note: "不允许远程调用"})
			writeJSON(w, 403, proto.Error{Error: "这个接口不能远程调用", Code: proto.CodeDenied})
			return
		}
		if r.Method != "GET" {
			s.ops.add(opEntry{At: time.Now(), Who: who, Op: "admin", Target: r.Method + " " + rest, OK: true})
		}
		r2 := r.Clone(context.WithValue(r.Context(), actorKey{}, who))
		r2.URL.Path = "/api/" + rest
		r2.URL.RawPath = ""
		r2.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		api.ServeHTTP(w, r2)
	})
}
