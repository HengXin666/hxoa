package server

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseChecksAndApplicable(t *testing.T) {
	cs := parseChecks(`checks:
  - name: typecheck
    run: npm run typecheck
    paths: [protocol, server]
  - name: "go test"
    run: 'go test ./...'
    timeout: 30
  - name: broken
`)
	if len(cs) != 2 || cs[0].Run != "npm run typecheck" || !reflect.DeepEqual(cs[0].Paths, []string{"protocol", "server"}) || cs[1].Timeout != 30 || cs[1].Name != "go test" {
		t.Fatalf("parse: %+v", cs)
	}
	if got := applicable(cs, []string{"client/web/x.ts"}); len(got) != 1 || got[0].Name != "go test" {
		t.Fatalf("paths filter: %+v", got)
	}
	if got := applicable(cs, []string{"server/a.ts"}); len(got) != 2 {
		t.Fatalf("hit: %+v", got)
	}
	for cmd, want := range map[string]bool{"git commit -m x": true, "git add -A && git -c a=b commit -q": true, "git log": false, "echo commit": false} {
		if isCommit(cmd) != want {
			t.Fatalf("isCommit(%q)", cmd)
		}
	}
}

func TestGateScript(t *testing.T) {
	dir := t.TempDir()
	run := func(checks []Check) (int, string) {
		s, _ := gateScript(checks, "touch committed")
		cmd := exec.Command("bash", "-c", s)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		code := 0
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		}
		return code, string(out)
	}
	code, out := run([]Check{{Name: "ok", Run: "true", Timeout: 5}, {Name: "bad one", Run: "false", Timeout: 5}})
	if code != gateFailCode || !reflect.DeepEqual(failedOf(out), []string{"bad_one"}) {
		t.Fatalf("refused: %d %q", code, out)
	}
	if _, err := os.Stat(filepath.Join(dir, "committed")); err == nil {
		t.Fatal("command ran after a failed check")
	}
	if code, out := run([]Check{{Name: "ok", Run: "true", Timeout: 5}}); code != 0 {
		t.Fatalf("pass: %d %q", code, out)
	}
	if _, err := os.Stat(filepath.Join(dir, "committed")); err != nil {
		t.Fatal("command did not run")
	}
}

func TestClaimsAndAreas(t *testing.T) {
	got, err := cleanClaims([]string{"client/web/, server", "/client/web"})
	if err != nil || !reflect.DeepEqual(got, []string{"client/web", "server"}) {
		t.Fatalf("claims: %v %v", got, err)
	}
	if got, _ := cleanClaims([]string{"../x"}); !reflect.DeepEqual(got, []string{"x"}) {
		t.Fatalf("escape not clamped: %v", got)
	}
	if !under("client/web/a.ts", "client/web") || under("client/webx", "client/web") {
		t.Fatal("under")
	}
	if k := areaKeys("client/web/ui/x.tsx"); !reflect.DeepEqual(k, []string{"client", "client-web", "client-web-ui", "client-web-ui-x.tsx"}) {
		t.Fatalf("areas: %v", k)
	}
}

func TestMemoryInboxAndNotices(t *testing.T) {
	m, err := OpenMemory(filepath.Join(t.TempDir(), "mem"), "")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.SetRoute("ws1", "proj"); err != nil || m.Route("ws1") != "proj" || m.Route("other") != "" {
		t.Fatalf("route: %v", err)
	}
	if m.SetRoute("ws1", "../x") == nil {
		t.Fatal("bad dir accepted")
	}
	m.appendLine("proj", inboxFile, "- 2026-11-28 oa_2: 插件的界面只进插件空间", "t")
	m.appendLine("proj", inboxFile, "- 2026-11-28 oa_3: 截图默认暗色", "t")
	if err := m.Resolve("proj", 0, true, ""); err != nil {
		t.Fatal(err)
	}
	if err := m.Resolve("proj", 0, false, ""); err != nil {
		t.Fatal(err)
	}
	if len(m.Inbox("proj")) != 0 || !strings.Contains(m.read("proj", lessonsFile), "插件的界面只进插件空间") || strings.Contains(m.read("proj", lessonsFile), "暗色") {
		t.Fatalf("inbox %v lessons %q", m.Inbox("proj"), m.read("proj", lessonsFile))
	}
	os.MkdirAll(filepath.Join(m.Dir, "proj", areasDir), 0o700)
	os.WriteFile(filepath.Join(m.Dir, "proj", areasDir, "client-web.md"), []byte("只用 HX-UI 组件"), 0o600)

	st, _ := OpenStore(t.TempDir())
	ws := &Workspace{ID: "ws1", Name: "w"}
	a := &Token{ID: "a", Label: "oa_1", Workspace: "ws1", Task: &Task{Issues: []int{155}, Paths: []string{"client/web"}}}
	b := &Token{ID: "b", Label: "oa_2", Workspace: "ws1"}
	st.ws["ws1"] = ws
	st.tokens["sa"], st.tokens["sb"] = a, b
	s := &Server{Store: st, Memory: m}
	cb := &reqCtx{tok: b, ws: ws}
	got := s.notices(cb, "write", []string{"client/web/ui/x.tsx"})
	if len(got) != 2 || !strings.Contains(got[0], "只用 HX-UI") || !strings.Contains(got[1], "oa_1 认领了 client/web (#155)") {
		t.Fatalf("notices: %q", got)
	}
	if again := s.notices(cb, "write", []string{"client/web/ui/y.tsx"}); len(again) != 0 {
		t.Fatalf("repeated: %q", again)
	}
	if _, err := s.Say(cb, "改了 proto.Task, 加了 paths", ""); err != nil {
		t.Fatal(err)
	}
	if q := s.notices(&reqCtx{tok: a, ws: ws}, "read", nil); len(q) != 1 || !strings.Contains(q[0], "oa_2 说: 改了 proto.Task") {
		t.Fatalf("say: %q", q)
	}
	if len(m.Threads("proj", 10)) != 1 {
		t.Fatalf("threads: %v", m.Threads("proj", 10))
	}
	if br := s.Brief(ws, b); !strings.Contains(br, "oa_1 #155 认领 client/web") || !strings.Contains(br, "主人确认过的经验") {
		t.Fatalf("brief: %s", br)
	}
}
