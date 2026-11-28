package server

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

func TestIssueRefs(t *testing.T) {
	got := issueRefs("feat: 管理插件 (#104)\n\n顺带修 #108, 再提 #104 与 a#9 x/#7 &#38;")
	if !reflect.DeepEqual(got, []int{104, 108}) {
		t.Fatal(got)
	}
	for in, want := range map[string]string{
		"git@github.com:HengXin666/HX-AIOA.git":      "https://github.com/HengXin666/HX-AIOA",
		"https://github.com/HengXin666/hxoa":         "https://github.com/HengXin666/hxoa",
		"https://github.com/HengXin666/hx.oa.git/\n": "https://github.com/HengXin666/hx.oa",
		"https://gitee.com/a/b.git":                  "",
	} {
		if g := githubURL(in); g != want {
			t.Fatal(in, g)
		}
	}
	if task, err := cleanTask([]int{108, 104, 108}, " 说明 ", nil, "agent"); err != nil || !reflect.DeepEqual(task.Issues, []int{104, 108}) || task.Note != "说明" {
		t.Fatal(task, err)
	}
	if task, err := cleanTask(nil, "  ", nil, "agent"); task != nil || err != nil {
		t.Fatal("empty should clear", task, err)
	}
	if _, err := cleanTask([]int{-1}, "", nil, "agent"); err == nil {
		t.Fatal("negative issue")
	}
}

func run(t *testing.T, dir string, args ...string) {
	t.Helper()
	c := exec.Command(args[0], args[1:]...)
	c.Dir = dir
	c.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatal(args, string(out))
	}
}

func TestWorkerTask(t *testing.T) {
	s, root := remoteServer(t)
	repo := filepath.Join(root, "repo")
	os.MkdirAll(repo, 0o755)
	run(t, repo, "git", "init", "-q", "-b", "main")
	run(t, repo, "git", "remote", "add", "origin", "git@github.com:o/r.git")
	os.WriteFile(filepath.Join(repo, "a.txt"), []byte("a\n"), 0o644)
	run(t, repo, "git", "add", "-A")
	run(t, repo, "git", "commit", "-q", "-m", "init #1")
	ws, err := s.Store.AddWorkspace("r", repo, true, true)
	if err != nil {
		t.Fatal(err)
	}
	tok, _ := s.Store.AddToken(ws.ID, "oa_1")
	if w, out := adminDo(s, "POST", "/v1/task", tok.Secret, `{"issues":[108,104],"note":"插件"}`); w.Code != 200 || out["task"] == nil {
		t.Fatal("set task", w.Code, w.Body.String())
	}
	if w, out := adminDo(s, "GET", "/v1/hello", tok.Secret, ""); w.Code != 200 || out["task"].(map[string]any)["note"] != "插件" {
		t.Fatal("hello task", out)
	}
	tk, w2 := s.Store.TokenByID(tok.ID)
	if tk.Task == nil || !reflect.DeepEqual(tk.Task.Issues, []int{104, 108}) || tk.Task.By != "agent" {
		t.Fatal("stored", tk.Task)
	}
	dir := workerDir(w2, tk)
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b\n"), 0o644)
	run(t, dir, "git", "add", "-A")
	run(t, dir, "git", "commit", "-q", "-m", "feat: b (#104)\n\nrefs #110")
	if st := workerStatus(w2, tk); !reflect.DeepEqual(st.Refs, []int{104, 110}) {
		t.Fatal("refs", st.Refs)
	}
	run(t, repo, "git", "update-ref", "refs/remotes/origin/main", "hxoa/"+workerKey(tk))
	if st := workerStatus(w2, tk); len(st.Refs) != 0 {
		t.Fatal("pushed commits should not count", st.Refs)
	}
	if repoURL(repo) != "https://github.com/o/r" {
		t.Fatal("repo", repoURL(repo))
	}
	if w, out := adminDo(s, "POST", "/v1/task", tok.Secret, `{"issues":[]}`); w.Code != 200 || out["task"] != nil {
		t.Fatal("clear", w.Body.String())
	}
	if w, _ := adminDo(s, "POST", "/v1/task", tok.Secret, `{"issues":[0]}`); w.Code != 400 {
		t.Fatal("bad issue", w.Code)
	}
	admin, _, _ := s.Store.AddAdminToken("aioa")
	if w, out := adminDo(s, "PUT", "/v1/admin/tokens/"+tok.ID+"/task", admin, `{"issues":[115],"note":"派给你"}`); w.Code != 200 || out["task"].(map[string]any)["by"] != "user" {
		t.Fatal("panel assign", w.Code, w.Body.String())
	}
	if _, out := adminDo(s, "GET", "/v1/task", tok.Secret, ""); out["task"].(map[string]any)["note"] != "派给你" {
		t.Fatal("agent sees assignment", out)
	}
}
