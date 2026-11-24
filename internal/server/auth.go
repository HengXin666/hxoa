package server

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Panel authentication.
//
// Two modes:
//   - password (default): the panel has its own account. Sessions are signed
//     cookies, so they survive restarts; changing the password signs every
//     session out.
//   - none: hxoad does no panel auth and trusts whatever sits in front of it
//     (SafeLine's 身份认证, an SSO proxy, an ssh tunnel). Only use this when
//     the panel port is not reachable any other way.
//
// The account lives in <data>/auth.json. Until one is saved there, the
// HXOA_ADMIN_PASSWORD seed (user "admin") is accepted; with neither, a panel
// bound to localhost is open and asks to set an account.

const (
	AuthPassword = "password"
	AuthNone     = "none"

	sessionCookie = "hxoa_session"
	sessionTTL    = 7 * 24 * time.Hour
	pbkdfIter     = 210000
)

type authFile struct {
	Username string `json:"username"`
	Salt     string `json:"salt"`
	Hash     string `json:"hash"`
	Iter     int    `json:"iter"`
	Key      string `json:"key"` // HMAC key for session cookies
	Gen      int    `json:"gen"` // bumped on every account change
}

type Auth struct {
	Mode     string
	path     string
	seedPw   string
	loopback bool // panel listens on localhost only

	mu    sync.Mutex
	f     authFile
	fails map[string]*failCount
}

type failCount struct {
	n     int
	until time.Time
}

func NewAuth(dataDir, mode, seedPassword string, loopback bool) (*Auth, error) {
	if mode == "" {
		mode = AuthPassword
	}
	if mode != AuthPassword && mode != AuthNone {
		return nil, errors.New("panel-auth 只能是 password 或 none")
	}
	a := &Auth{Mode: mode, path: filepath.Join(dataDir, "auth.json"), seedPw: seedPassword, loopback: loopback, fails: map[string]*failCount{}}
	if b, err := os.ReadFile(a.path); err == nil {
		if err := json.Unmarshal(b, &a.f); err != nil {
			return nil, err
		}
	}
	if a.f.Key == "" {
		a.f.Key = randHex(32)
		if err := a.save(); err != nil {
			return nil, err
		}
	}
	return a, nil
}

func (a *Auth) save() error {
	b, _ := json.MarshalIndent(a.f, "", "  ")
	if err := os.MkdirAll(filepath.Dir(a.path), 0o700); err != nil {
		return err
	}
	tmp := a.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, a.path)
}

// hasAccount reports whether any credential is configured.
func (a *Auth) hasAccount() bool { return a.f.Hash != "" || a.seedPw != "" }

// HasAccount is hasAccount for callers outside the package.
func (a *Auth) HasAccount() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.hasAccount()
}

func (a *Auth) username() string {
	if a.f.Hash != "" {
		return a.f.Username
	}
	if a.seedPw != "" {
		return "admin"
	}
	return ""
}

// open: no login needed at all.
func (a *Auth) open() bool {
	return a.Mode == AuthNone || (!a.hasAccount() && a.loopback)
}

func (a *Auth) check(user, pw string) bool {
	if a.f.Hash != "" {
		salt, _ := hex.DecodeString(a.f.Salt)
		want, _ := hex.DecodeString(a.f.Hash)
		got := pbkdf2(pw, salt, a.f.Iter)
		return subtle.ConstantTimeCompare([]byte(user), []byte(a.f.Username)) == 1 && subtle.ConstantTimeCompare(got, want) == 1
	}
	if a.seedPw != "" {
		return subtle.ConstantTimeCompare([]byte(user), []byte("admin")) == 1 && subtle.ConstantTimeCompare([]byte(pw), []byte(a.seedPw)) == 1
	}
	return false
}

func (a *Auth) setAccount(user, pw string) error {
	user = strings.TrimSpace(user)
	if user == "" {
		return errors.New("用户名不能为空")
	}
	if len(pw) < 8 {
		return errors.New("密码至少 8 位")
	}
	salt := make([]byte, 16)
	rand.Read(salt)
	a.f.Username = user
	a.f.Salt = hex.EncodeToString(salt)
	a.f.Iter = pbkdfIter
	a.f.Hash = hex.EncodeToString(pbkdf2(pw, salt, pbkdfIter))
	a.f.Gen++
	return a.save()
}

// Session cookie: base64(user) "." expiry "." gen "." hmac.
func (a *Auth) mint(user string) string {
	exp := time.Now().Add(sessionTTL).Unix()
	body := base64.RawURLEncoding.EncodeToString([]byte(user)) + "." + strconv.FormatInt(exp, 10) + "." + strconv.Itoa(a.f.Gen)
	return body + "." + a.sign(body)
}

func (a *Auth) sign(s string) string {
	k, _ := hex.DecodeString(a.f.Key)
	m := hmac.New(sha256.New, k)
	m.Write([]byte(s))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

func (a *Auth) session(r *http.Request) (string, bool) {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return "", false
	}
	p := strings.Split(c.Value, ".")
	if len(p) != 4 {
		return "", false
	}
	body := strings.Join(p[:3], ".")
	if subtle.ConstantTimeCompare([]byte(a.sign(body)), []byte(p[3])) != 1 {
		return "", false
	}
	exp, _ := strconv.ParseInt(p[1], 10, 64)
	gen, _ := strconv.Atoi(p[2])
	if time.Now().Unix() > exp || gen != a.f.Gen {
		return "", false
	}
	u, _ := base64.RawURLEncoding.DecodeString(p[0])
	if string(u) != a.username() {
		return "", false
	}
	return string(u), true
}

// Authorized reports whether the request may use the panel API.
func (a *Auth) Authorized(r *http.Request) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.open() {
		return true
	}
	_, ok := a.session(r)
	return ok
}

func setCookie(w http.ResponseWriter, r *http.Request, v string, maxAge int) {
	secure := r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: v, Path: "/", MaxAge: maxAge, HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode})
}

// Routes adds /api/auth, /api/login, /api/logout and /api/account.
func (a *Auth) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/auth", func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		defer a.mu.Unlock()
		user, ok := a.session(r)
		if a.open() {
			ok = true
		}
		writeJSON(w, 200, map[string]any{
			"ok":          true,
			"mode":        a.Mode,
			"logged_in":   ok,
			"user":        user,
			"username":    a.username(),
			"has_account": a.hasAccount(),
			"seeded":      a.f.Hash == "" && a.seedPw != "",
		})
	})
	mux.HandleFunc("POST /api/login", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Username, Password string }
		json.NewDecoder(r.Body).Decode(&in)
		ip := clientIP(r)
		a.mu.Lock()
		defer a.mu.Unlock()
		if f := a.fails[ip]; f != nil && time.Now().Before(f.until) {
			writeJSON(w, 429, map[string]any{"ok": false, "error": "尝试次数过多, 请 " + strconv.Itoa(int(time.Until(f.until).Seconds())+1) + " 秒后再试"})
			return
		}
		if a.Mode == AuthNone {
			writeJSON(w, 400, map[string]any{"ok": false, "error": "面板认证已交给上游, 无需登录"})
			return
		}
		if !a.check(strings.TrimSpace(in.Username), in.Password) {
			f := a.fails[ip]
			if f == nil {
				f = &failCount{}
				a.fails[ip] = f
			}
			f.n++
			if f.n >= 5 {
				f.until = time.Now().Add(time.Duration(f.n-4) * time.Minute)
			}
			writeJSON(w, 401, map[string]any{"ok": false, "error": "用户名或密码错误"})
			return
		}
		delete(a.fails, ip)
		setCookie(w, r, a.mint(a.username()), int(sessionTTL.Seconds()))
		writeJSON(w, 200, map[string]any{"ok": true, "user": a.username()})
	})
	mux.HandleFunc("POST /api/logout", func(w http.ResponseWriter, r *http.Request) {
		setCookie(w, r, "", -1)
		writeJSON(w, 200, map[string]any{"ok": true})
	})
	mux.HandleFunc("POST /api/account", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Username    string `json:"username"`
			OldPassword string `json:"old_password"`
			NewPassword string `json:"new_password"`
		}
		json.NewDecoder(r.Body).Decode(&in)
		a.mu.Lock()
		defer a.mu.Unlock()
		if a.Mode == AuthNone {
			writeJSON(w, 400, map[string]any{"ok": false, "error": "当前为 none 模式, 面板认证由上游负责"})
			return
		}
		_, logged := a.session(r)
		if a.hasAccount() {
			if !logged {
				writeJSON(w, 401, map[string]any{"ok": false, "error": "请先登录"})
				return
			}
			if !a.check(a.username(), in.OldPassword) {
				writeJSON(w, 400, map[string]any{"ok": false, "error": "当前密码不对"})
				return
			}
		} else if !a.loopback {
			writeJSON(w, 403, map[string]any{"ok": false, "error": "只能在本机面板上首次设置账号"})
			return
		}
		if err := a.setAccount(in.Username, in.NewPassword); err != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		setCookie(w, r, a.mint(a.username()), int(sessionTTL.Seconds()))
		writeJSON(w, 200, map[string]any{"ok": true, "user": a.username()})
	})
}

// pbkdf2 is PBKDF2-HMAC-SHA256 with a 32-byte key (stdlib only until Go 1.24).
func pbkdf2(pw string, salt []byte, iter int) []byte {
	prf := hmac.New(sha256.New, []byte(pw))
	prf.Write(salt)
	var idx [4]byte
	binary.BigEndian.PutUint32(idx[:], 1)
	prf.Write(idx[:])
	u := prf.Sum(nil)
	out := append([]byte(nil), u...)
	for i := 1; i < iter; i++ {
		prf.Reset()
		prf.Write(u)
		u = prf.Sum(u[:0])
		for j := range out {
			out[j] ^= u[j]
		}
	}
	return out
}
