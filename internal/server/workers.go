package server

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// Worker mode: each token works in its own git worktree on branch
// hxoa/<label>-<token id>, at <workspace>/.hxoa/workers/<label>-<token id>.
// The id keeps two tokens with the same label (or a label reused after a
// token was deleted) from sharing a branch. The user's checkout is
// never touched until they merge from the panel.

var worktreeMu sync.Mutex

// ready caches worktrees known to exist, so the hot path takes no lock and
// runs no git.
var ready sync.Map // dir -> base branch at creation

func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	s := strings.TrimSpace(string(out))
	if err != nil {
		if s == "" {
			s = err.Error()
		}
		return s, errors.New(s)
	}
	return s, nil
}

func isGitRepo(dir string) bool {
	out, err := git(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return false
	}
	a, _ := filepath.EvalSymlinks(out)
	b, _ := filepath.EvalSymlinks(dir)
	return a == b
}

var unsafeRef = regexp.MustCompile(`[^\p{L}\p{N}._-]+`)

// workerKey names a token's branch and worktree: "<label>-<id>".
func workerKey(t *Token) string {
	l := strings.Trim(unsafeRef.ReplaceAllString(t.Label, "-"), "-.")
	if l == "" {
		l = "worker"
	}
	return l + "-" + t.ID
}

func workerBranch(t *Token) string { return "hxoa/" + workerKey(t) }

func workerDir(ws *Workspace, t *Token) string {
	return filepath.Join(ws.Path, ".hxoa", "workers", workerKey(t))
}

func excludeHxoa(ws *Workspace) {
	gitDir, err := git(ws.Path, "rev-parse", "--git-common-dir")
	if err != nil {
		return
	}
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(ws.Path, gitDir)
	}
	p := filepath.Join(gitDir, "info", "exclude")
	b, _ := os.ReadFile(p)
	if strings.Contains(string(b), "/.hxoa/") {
		return
	}
	os.MkdirAll(filepath.Dir(p), 0o755)
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err == nil {
		f.WriteString("\n/.hxoa/\n")
		f.Close()
	}
}

// ensureWorker returns the root a token should operate in, creating the
// worktree on first use.
func ensureWorker(ws *Workspace, t *Token) (root, branch, base string, err error) {
	if !ws.Workers {
		return ws.Path, "", "", nil
	}
	dir := workerDir(ws, t)
	branch = workerBranch(t)
	if b, ok := ready.Load(dir); ok {
		if _, statErr := os.Stat(filepath.Join(dir, ".git")); statErr == nil {
			return dir, branch, b.(string), nil
		}
		ready.Delete(dir)
	}
	worktreeMu.Lock()
	defer worktreeMu.Unlock()
	base, _ = git(ws.Path, "rev-parse", "--abbrev-ref", "HEAD")
	if _, statErr := os.Stat(filepath.Join(dir, ".git")); statErr == nil {
		ready.Store(dir, base)
		return dir, branch, base, nil
	}
	excludeHxoa(ws)
	os.MkdirAll(filepath.Dir(dir), 0o755)
	git(ws.Path, "worktree", "prune")
	if _, e := git(ws.Path, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch); e == nil {
		_, err = git(ws.Path, "worktree", "add", dir, branch)
	} else {
		_, err = git(ws.Path, "worktree", "add", "-b", branch, dir, "HEAD")
	}
	if err != nil {
		return "", "", "", errors.New("创建 worker 工作树失败: " + err.Error())
	}
	ready.Store(dir, base)
	return dir, branch, base, nil
}

type WorkerStatus struct {
	Label   string   `json:"label"`
	Branch  string   `json:"branch"`
	Exists  bool     `json:"exists"`
	Ahead   string   `json:"ahead"`  // commits on worker not on base
	Behind  string   `json:"behind"` // commits on base not on worker
	Dirty   []string `json:"dirty"`  // uncommitted files in the worktree
	BaseRef string   `json:"base"`
}

func workerStatus(ws *Workspace, t *Token) WorkerStatus {
	st := WorkerStatus{Label: t.Label, Branch: workerBranch(t)}
	dir := workerDir(ws, t)
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		return st
	}
	st.Exists = true
	st.BaseRef, _ = git(ws.Path, "rev-parse", "--abbrev-ref", "HEAD")
	if out, err := git(ws.Path, "rev-list", "--left-right", "--count", "HEAD..."+st.Branch); err == nil {
		f := strings.Fields(out)
		if len(f) == 2 {
			st.Behind, st.Ahead = f[0], f[1]
		}
	}
	if out, err := git(dir, "status", "--porcelain"); err == nil && out != "" {
		for _, l := range strings.Split(out, "\n") {
			st.Dirty = append(st.Dirty, strings.TrimSpace(l))
		}
	}
	return st
}

// mergeWorker commits whatever the worker left uncommitted, then merges its
// branch into the user's current branch. On conflict it aborts and reports,
// leaving the user's checkout exactly as it was.
func mergeWorker(ws *Workspace, t *Token) (string, error) {
	label := t.Label
	worktreeMu.Lock()
	defer worktreeMu.Unlock()
	dir := workerDir(ws, t)
	branch := workerBranch(t)
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		return "", errors.New("这个 worker 还没有工作树")
	}
	if out, _ := git(dir, "status", "--porcelain"); out != "" {
		git(dir, "add", "-A")
		if _, err := git(dir, "-c", "user.name=hxoa", "-c", "user.email=hxoa@localhost", "commit", "-m", "hxoa: "+label+" 的改动"); err != nil {
			return "", errors.New("提交 worker 改动失败: " + err.Error())
		}
	}
	if out, _ := git(ws.Path, "status", "--porcelain", "--untracked-files=no"); out != "" {
		return "", errors.New("你的工作区有未提交的修改, 为了不弄乱它, 请先提交或 stash 再合并:\n" + out)
	}
	out, err := git(ws.Path, "-c", "user.name=hxoa", "-c", "user.email=hxoa@localhost", "merge", "--no-ff", "--no-edit", "-m", "hxoa: 合并 "+label, branch)
	if err != nil {
		git(ws.Path, "merge", "--abort")
		return "", errors.New("合并有冲突, 已撤销, 你的工作区没变。可以让 " + label + " 在它的分支里先 git merge " + ws.Branch() + " 解决冲突再合并:\n" + out)
	}
	// Bring the worker up to date so it continues from the merged state.
	git(dir, "merge", "--ff-only", ws.Branch())
	return out, nil
}

// Branch is the user's current branch name.
func (w *Workspace) Branch() string {
	// symbolic-ref also works in a repo with no commits yet.
	b, err := git(w.Path, "symbolic-ref", "--short", "-q", "HEAD")
	if err != nil {
		if h, err := git(w.Path, "rev-parse", "--short", "HEAD"); err == nil {
			return h // detached
		}
		return ""
	}
	return b
}

func removeWorker(ws *Workspace, t *Token) {
	worktreeMu.Lock()
	defer worktreeMu.Unlock()
	dir := workerDir(ws, t)
	ready.Delete(dir)
	if _, err := os.Stat(dir); err == nil {
		git(ws.Path, "worktree", "remove", "--force", dir)
	}
	// Drop the branch only when everything on it is already merged; `-d`
	// refuses otherwise, so unmerged work survives the token.
	git(ws.Path, "branch", "-d", workerBranch(t))
}
