package server

import (
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPBKDF2Vector(t *testing.T) {
	got := hex.EncodeToString(pbkdf2("password", []byte("salt"), 1))
	if got != "120fb6cffcf8b32c43e7225256c4f837a86548c92ccc35480805987cb70be17b" {
		t.Fatal(got)
	}
	got = hex.EncodeToString(pbkdf2("password", []byte("salt"), 4096))
	if got != "c5e478d59288c841aa530db6845c4c8d962893a001ce4e11a4963873aa98134a" {
		t.Fatal(got)
	}
}

func authMux(t *testing.T, a *Auth) *http.ServeMux {
	mux := http.NewServeMux()
	a.Routes(mux)
	mux.HandleFunc("GET /api/secret", func(w http.ResponseWriter, r *http.Request) {
		if !a.Authorized(r) {
			w.WriteHeader(401)
			return
		}
		w.WriteHeader(200)
	})
	return mux
}

func do(mux http.Handler, method, path, body string, cookies []*http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	for _, c := range cookies {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

func TestAuthFlow(t *testing.T) {
	dir := t.TempDir()
	a, err := NewAuth(dir, AuthPassword, "seed-password", false)
	if err != nil {
		t.Fatal(err)
	}
	mux := authMux(t, a)
	if w := do(mux, "GET", "/api/secret", "", nil); w.Code != 401 {
		t.Fatal("open without login", w.Code)
	}
	if w := do(mux, "POST", "/api/login", `{"username":"admin","password":"nope"}`, nil); w.Code != 401 {
		t.Fatal("bad password accepted")
	}
	w := do(mux, "POST", "/api/login", `{"username":"admin","password":"seed-password"}`, nil)
	if w.Code != 200 {
		t.Fatal("seed login", w.Body.String())
	}
	old := w.Result().Cookies()
	if w := do(mux, "GET", "/api/secret", "", old); w.Code != 200 {
		t.Fatal("session rejected")
	}
	// change account: old sessions die, the new one works, the seed stops working
	w = do(mux, "POST", "/api/account", `{"username":"loli","old_password":"seed-password","new_password":"n3w-password!"}`, old)
	if w.Code != 200 {
		t.Fatal("change", w.Body.String())
	}
	fresh := w.Result().Cookies()
	if w := do(mux, "GET", "/api/secret", "", old); w.Code != 401 {
		t.Fatal("old session survived password change")
	}
	if w := do(mux, "GET", "/api/secret", "", fresh); w.Code != 200 {
		t.Fatal("new session rejected")
	}
	if w := do(mux, "POST", "/api/login", `{"username":"admin","password":"seed-password"}`, nil); w.Code != 401 {
		t.Fatal("seed still works after saving an account")
	}
	// survives a restart
	b, err := NewAuth(dir, AuthPassword, "seed-password", false)
	if err != nil {
		t.Fatal(err)
	}
	if w := do(authMux(t, b), "GET", "/api/secret", "", fresh); w.Code != 200 {
		t.Fatal("session lost on restart")
	}
	if w := do(authMux(t, b), "POST", "/api/login", `{"username":"loli","password":"n3w-password!"}`, nil); w.Code != 200 {
		t.Fatal("new account login", w.Body.String())
	}
}

func TestAuthLockout(t *testing.T) {
	a, _ := NewAuth(t.TempDir(), AuthPassword, "seed-password", false)
	mux := authMux(t, a)
	for i := 0; i < 5; i++ {
		do(mux, "POST", "/api/login", `{"username":"admin","password":"x"}`, nil)
	}
	if w := do(mux, "POST", "/api/login", `{"username":"admin","password":"seed-password"}`, nil); w.Code != 429 {
		t.Fatal("no lockout", w.Code)
	}
}

func TestAuthNone(t *testing.T) {
	a, _ := NewAuth(t.TempDir(), AuthNone, "", false)
	mux := authMux(t, a)
	if w := do(mux, "GET", "/api/secret", "", nil); w.Code != 200 {
		t.Fatal("none mode should not ask for login")
	}
	if w := do(mux, "POST", "/api/account", `{"username":"x","new_password":"12345678"}`, nil); w.Code != 400 {
		t.Fatal("none mode let an account be set")
	}
}

func TestAuthFirstRunLocalOnly(t *testing.T) {
	remote, _ := NewAuth(t.TempDir(), AuthPassword, "", false)
	if w := do(authMux(t, remote), "POST", "/api/account", `{"username":"x","new_password":"12345678"}`, nil); w.Code != 403 {
		t.Fatal("remote first-run setup allowed", w.Code)
	}
	local, _ := NewAuth(t.TempDir(), AuthPassword, "", true)
	mux := authMux(t, local)
	if w := do(mux, "GET", "/api/secret", "", nil); w.Code != 200 {
		t.Fatal("local panel with no account should be open")
	}
	if w := do(mux, "POST", "/api/account", `{"username":"me","new_password":"12345678"}`, nil); w.Code != 200 {
		t.Fatal("local setup", w.Body.String())
	}
	if w := do(mux, "GET", "/api/secret", "", nil); w.Code != 401 {
		t.Fatal("still open after setting an account")
	}
}
