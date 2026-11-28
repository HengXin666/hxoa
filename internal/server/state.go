package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Workspace is one local directory the user shares.
type Workspace struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	Path    string    `json:"path"`
	Workers bool      `json:"workers"` // each token works in its own git worktree
	Exec    bool      `json:"exec"`    // allow command execution
	Created time.Time `json:"created"`
}

// Token is one connection identity. Deleting it cuts that agent off at once and
// drops it from the connection count.
type Token struct {
	ID        string      `json:"id"`
	Secret    string      `json:"secret"`
	Workspace string      `json:"workspace"`
	Label     string      `json:"label"` // who the user says this is: "hark", "claude-local"
	Created   time.Time   `json:"created"`
	LastSeen  time.Time   `json:"last_seen,omitempty"`
	LastIP    string      `json:"last_ip,omitempty"`
	Ops       int64       `json:"ops"`
	Bye       bool        `json:"bye,omitempty"` // client logged out; next request revives it
	Task      *Task       `json:"task,omitempty"`
	Check     *CheckState `json:"check,omitempty"` // last commit gate
}

// TunnelConfig is how hxoad exposes the agent API. The Cloudflare API token
// used for automatic setup is never stored; only the resulting tunnel token.
type TunnelConfig struct {
	Mode     string `json:"mode"`               // off | quick | token | external
	Token    string `json:"token,omitempty"`    // cloudflared tunnel run token
	Hostname string `json:"hostname,omitempty"` // public hostname for token mode
	TunnelID string `json:"tunnel_id,omitempty"`
}

type stateFile struct {
	Workspaces []*Workspace  `json:"workspaces"`
	Tokens     []*Token      `json:"tokens"`
	Tunnel     *TunnelConfig `json:"tunnel,omitempty"`
	Admins     []*AdminToken `json:"admin_tokens,omitempty"`
}

// Store keeps state in memory and persists it to one JSON file.
type Store struct {
	mu        sync.RWMutex
	path      string
	ws        map[string]*Workspace
	tokens    map[string]*Token // by secret
	tunnel    TunnelConfig
	admins    map[string]*AdminToken // by secret hash
	dirty     bool
	OnlineFor time.Duration
}

func OpenStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	s := &Store{
		path:      filepath.Join(dir, "state.json"),
		ws:        map[string]*Workspace{},
		tokens:    map[string]*Token{},
		admins:    map[string]*AdminToken{},
		OnlineFor: 3 * time.Minute,
	}
	raw, err := os.ReadFile(s.path)
	if err == nil {
		var f stateFile
		if err := json.Unmarshal(raw, &f); err != nil {
			return nil, err
		}
		for _, w := range f.Workspaces {
			s.ws[w.ID] = w
		}
		for _, t := range f.Tokens {
			s.tokens[t.Secret] = t
		}
		if f.Tunnel != nil {
			s.tunnel = *f.Tunnel
		}
		for _, a := range f.Admins {
			s.admins[a.Hash] = a
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	go s.flushLoop()
	return s, nil
}

func (s *Store) flushLoop() {
	for range time.Tick(5 * time.Second) {
		s.mu.Lock()
		if s.dirty {
			s.saveLocked()
		}
		s.mu.Unlock()
	}
}

func (s *Store) saveLocked() error {
	tc := s.tunnel
	f := stateFile{Tunnel: &tc}
	for _, w := range s.ws {
		f.Workspaces = append(f.Workspaces, w)
	}
	for _, t := range s.tokens {
		f.Tokens = append(f.Tokens, t)
	}
	for _, a := range s.admins {
		f.Admins = append(f.Admins, a)
	}
	sort.Slice(f.Admins, func(i, j int) bool { return f.Admins[i].Created.Before(f.Admins[j].Created) })
	sort.Slice(f.Workspaces, func(i, j int) bool { return f.Workspaces[i].Created.Before(f.Workspaces[j].Created) })
	sort.Slice(f.Tokens, func(i, j int) bool { return f.Tokens[i].Created.Before(f.Tokens[j].Created) })
	raw, _ := json.MarshalIndent(f, "", "  ")
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	s.dirty = false
	return os.Rename(tmp, s.path)
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

var labelRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,31}$`)

// Auth resolves a bearer secret and records the visit.
func (s *Store) Auth(secret, ip string) (*Token, *Workspace) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.tokens[secret]
	if t == nil {
		return nil, nil
	}
	w := s.ws[t.Workspace]
	if w == nil {
		return nil, nil
	}
	t.LastSeen = time.Now()
	t.LastIP = ip
	t.Ops++
	t.Bye = false
	s.dirty = true
	cw, ct := *w, *t
	return &ct, &cw
}

func (s *Store) Bye(secret string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t := s.tokens[secret]; t != nil {
		t.Bye = true
		s.dirty = true
	}
}

func (s *Store) Online(t *Token) bool {
	return !t.Bye && !t.LastSeen.IsZero() && time.Since(t.LastSeen) < s.OnlineFor
}

func (s *Store) AddWorkspace(name, path string, workers, exec bool) (*Workspace, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	st, err := os.Stat(abs)
	if err != nil || !st.IsDir() {
		return nil, errors.New("目录不存在: " + abs)
	}
	if abs == "/" {
		return nil, errors.New("不能把根目录 / 当作工作区")
	}
	if dd, _ := filepath.Abs(filepath.Dir(s.path)); dd == abs || strings.HasPrefix(dd, abs+string(os.PathSeparator)) {
		return nil, errors.New("工作区不能包含 hxoad 的数据目录 " + dd + " (里面有令牌)")
	}
	if name == "" {
		name = filepath.Base(abs)
	}
	if workers && !isGitRepo(abs) {
		return nil, errors.New("workers 需要工作区是 git 仓库")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, w := range s.ws {
		if w.Path == abs {
			return nil, errors.New("这个目录已经是工作区: " + w.Name)
		}
	}
	w := &Workspace{ID: randHex(4), Name: name, Path: abs, Workers: workers, Exec: exec, Created: time.Now()}
	s.ws[w.ID] = w
	s.dirty = true
	s.saveLocked()
	return w, nil
}

func (s *Store) UpdateWorkspace(id string, name *string, workers, exec *bool) (*Workspace, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w := s.ws[id]
	if w == nil {
		return nil, errors.New("工作区不存在")
	}
	if workers != nil && *workers && !isGitRepo(w.Path) {
		return nil, errors.New("workers 需要工作区是 git 仓库")
	}
	if name != nil && *name != "" {
		w.Name = *name
	}
	if workers != nil {
		w.Workers = *workers
	}
	if exec != nil {
		w.Exec = *exec
	}
	s.dirty = true
	s.saveLocked()
	c := *w
	return &c, nil
}

func (s *Store) DeleteWorkspace(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.ws, id)
	for k, t := range s.tokens {
		if t.Workspace == id {
			delete(s.tokens, k)
		}
	}
	s.dirty = true
	s.saveLocked()
}

func (s *Store) AddToken(wsID, label string) (*Token, error) {
	if !labelRe.MatchString(label) {
		return nil, errors.New("身份名只能用字母数字 . _ -, 最长 32")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ws[wsID] == nil {
		return nil, errors.New("工作区不存在")
	}
	for _, t := range s.tokens {
		if t.Workspace == wsID && t.Label == label {
			return nil, errors.New("这个工作区已经有叫 " + label + " 的连接")
		}
	}
	t := &Token{ID: randHex(4), Secret: "hx_" + randHex(16), Workspace: wsID, Label: label, Created: time.Now()}
	s.tokens[t.Secret] = t
	s.dirty = true
	s.saveLocked()
	c := *t
	return &c, nil
}

func (s *Store) DeleteToken(id string) *Token {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, t := range s.tokens {
		if t.ID == id {
			delete(s.tokens, k)
			s.dirty = true
			s.saveLocked()
			return t
		}
	}
	return nil
}

func (s *Store) TokenByID(id string) (*Token, *Workspace) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, t := range s.tokens {
		if t.ID == id {
			ct := *t
			if w := s.ws[t.Workspace]; w != nil {
				cw := *w
				return &ct, &cw
			}
			return &ct, nil
		}
	}
	return nil, nil
}

func (s *Store) Workspace(id string) *Workspace {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if w := s.ws[id]; w != nil {
		c := *w
		return &c
	}
	return nil
}

// Snapshot returns copies for the admin view.
func (s *Store) Snapshot() ([]Workspace, []Token) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var ws []Workspace
	var ts []Token
	for _, w := range s.ws {
		ws = append(ws, *w)
	}
	for _, t := range s.tokens {
		ts = append(ts, *t)
	}
	sort.Slice(ws, func(i, j int) bool { return ws[i].Created.Before(ws[j].Created) })
	sort.Slice(ts, func(i, j int) bool { return ts[i].Created.Before(ts[j].Created) })
	return ws, ts
}

func (s *Store) Tunnel() TunnelConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.tunnel
}

func (s *Store) SetTunnel(c TunnelConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tunnel = c
	s.dirty = true
	s.saveLocked()
}

// DataDir is where state, logs and a downloaded cloudflared live.
func (s *Store) DataDir() string { return filepath.Dir(s.path) }

// SetCheck records the last commit gate of a connection.
func (s *Store) SetCheck(id string, c *CheckState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, tok := range s.tokens {
		if tok.ID == id {
			tok.Check = c
			s.dirty = true
			return
		}
	}
}
