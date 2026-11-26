package server

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func remoteServer(t *testing.T) (*Server, string) {
	t.Helper()
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	tun := NewTunnelManager(store, "127.0.0.1:7070", "https://hx.example", "")
	return &Server{Store: store, Tunnel: tun}, t.TempDir()
}

func adminDo(s *Server, method, path, token, body string) (*httptest.ResponseRecorder, map[string]any) {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.RemoteAddr = "203.0.113.9:1234"
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	s.AgentHandler().ServeHTTP(w, r)
	var out map[string]any
	json.Unmarshal(w.Body.Bytes(), &out)
	return w, out
}

func TestRemoteAdmin(t *testing.T) {
	s, root := remoteServer(t)
	if w, _ := adminDo(s, "GET", "/admin/v1/state", "", ""); w.Code != 401 {
		t.Fatal("no token", w.Code)
	}
	secret, a, err := s.Store.AddAdminToken("aioa")
	if err != nil || !strings.HasPrefix(secret, adminSecretPre) || a.Hash == "" {
		t.Fatal(err, secret)
	}
	raw, _ := os.ReadFile(filepath.Join(s.Store.DataDir(), "state.json"))
	if strings.Contains(string(raw), secret) {
		t.Fatal("secret stored in plain text")
	}
	if w, out := adminDo(s, "GET", "/admin/v1/hello", secret, ""); w.Code != 200 || out["admin"] != "aioa" {
		t.Fatal("hello", w.Code, out)
	}
	if w, _ := adminDo(s, "POST", "/admin/v1/mkdir", secret, `{"parent":"`+root+`","name":"proj"}`); w.Code != 200 {
		t.Fatal("mkdir", w.Code, w.Body.String())
	}
	w, out := adminDo(s, "POST", "/admin/v1/workspaces", secret, `{"name":"p","path":"`+filepath.Join(root, "proj")+`","exec":false}`)
	if w.Code != 200 {
		t.Fatal("add workspace", w.Code, w.Body.String())
	}
	wsID := out["workspace"].(map[string]any)["id"].(string)
	w, out = adminDo(s, "POST", "/admin/v1/workspaces/"+wsID+"/tokens", secret, `{"label":"hark"}`)
	if w.Code != 200 || !strings.Contains(out["prompt"].(string), "https://hx.example") {
		t.Fatal("token", w.Code, out)
	}
	tokID := out["token"].(map[string]any)["id"].(string)
	if w, out := adminDo(s, "GET", "/admin/v1/tokens/"+tokID+"/prompt", secret, ""); w.Code != 200 || out["prompt"] == "" {
		t.Fatal("prompt", w.Code)
	}
	for _, p := range [][2]string{{"POST", "/admin/v1/admin-tokens"}, {"POST", "/admin/v1/tunnel/token"}, {"POST", "/admin/v1/account"}, {"GET", "/admin/v1/admin-tokens"}} {
		if w, _ := adminDo(s, p[0], p[1], secret, `{}`); w.Code != 403 {
			t.Fatal("should be refused", p, w.Code)
		}
	}
	if w, _ := adminDo(s, "DELETE", "/admin/v1/tokens/"+tokID, secret, ""); w.Code != 200 {
		t.Fatal("delete token", w.Code)
	}
	if w, _ := adminDo(s, "DELETE", "/admin/v1/workspaces/"+wsID, secret, ""); w.Code != 200 {
		t.Fatal("delete ws", w.Code)
	}
	s.Store.DeleteAdminToken(a.ID)
	if w, _ := adminDo(s, "GET", "/admin/v1/state", secret, ""); w.Code != 401 {
		t.Fatal("revoked token still works", w.Code)
	}
}

func TestRemoteAdminLockout(t *testing.T) {
	s, _ := remoteServer(t)
	secret, _, _ := s.Store.AddAdminToken("aioa")
	for i := 0; i < remoteFailLimit; i++ {
		adminDo(s, "GET", "/admin/v1/state", "hxadm_wrong", "")
	}
	if w, _ := adminDo(s, "GET", "/admin/v1/state", secret, ""); w.Code != 429 {
		t.Fatal("expected lockout", w.Code)
	}
}
