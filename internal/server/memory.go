package server

import (
	"errors"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Memory is the collaboration memory: a git repo on this machine, optionally
// mirrored to a private remote, kept apart from the workspaces so it works
// whether or not a workspace is a git repo. Layout:
//
//	routes.yml              workspace id -> project dir (set from the panel)
//	<project>/brief.md      <=60 lines: what it is, boundaries, acceptance
//	<project>/areas/*.md    notes per directory ("client-web.md" for client/web)
//	<project>/lessons.md    lessons the user adopted, one per line
//	<project>/inbox.md      `hxoa learn` lands here until adopted or dropped
//	<project>/checks.yml    commands run before every commit
//	<project>/threads/*.md  `hxoa say` messages, one file per day
//
// hxoa itself carries no project content; it only reads, writes and serves.
type Memory struct {
	Dir    string
	Remote string
	mu     sync.Mutex
}

const (
	routesFile   = "routes.yml"
	briefFile    = "brief.md"
	lessonsFile  = "lessons.md"
	inboxFile    = "inbox.md"
	checksFile   = "checks.yml"
	areasDir     = "areas"
	threadsDir   = "threads"
	BriefMaxLine = 60
	pullEvery    = 5 * time.Minute
)

var projectNameRe = regexp.MustCompile(`^[\p{L}\p{N}._-]{1,64}$`)

// OpenMemory clones the remote (when given and the dir is new) or inits a
// local repo, then keeps pulling in the background.
func OpenMemory(dir, remote string) (*Memory, error) {
	m := &Memory{Dir: dir, Remote: remote}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		entries, _ := os.ReadDir(dir)
		if remote != "" && len(entries) == 0 {
			if out, err := git(filepath.Dir(dir), "clone", "-q", remote, dir); err != nil {
				return nil, errors.New("克隆协作记忆失败: " + out)
			}
		} else if out, err := git(dir, "init", "-q"); err != nil {
			return nil, errors.New("初始化协作记忆失败: " + out)
		} else if remote != "" {
			git(dir, "remote", "add", "origin", remote)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, routesFile)); err != nil {
		os.WriteFile(filepath.Join(dir, routesFile), []byte("# 工作区 id: 记忆目录 (面板里配)\n"), 0o600)
		m.commit("hxoad: 初始化 routes.yml")
	}
	go m.pullLoop()
	return m, nil
}

func (m *Memory) hasOrigin() bool {
	_, err := git(m.Dir, "remote", "get-url", "origin")
	return err == nil
}

func (m *Memory) pullLoop() {
	for {
		m.pull()
		time.Sleep(pullEvery)
	}
}

func (m *Memory) pull() {
	if !m.hasOrigin() {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if out, err := git(m.Dir, "pull", "-q", "--rebase", "--autostash"); err != nil && !strings.Contains(out, "couldn't find remote ref") {
		log.Printf("协作记忆 pull 失败: %s", out)
	}
}

// commit records everything changed; pushing happens in the background.
func (m *Memory) commit(msg string) {
	git(m.Dir, "add", "-A")
	if _, err := git(m.Dir, "-c", "user.name=hxoad", "-c", "user.email=hxoad@localhost", "commit", "-q", "-m", msg); err != nil {
		return
	}
	m.pushAsync()
}

func (m *Memory) pushAsync() {
	if m.hasOrigin() {
		go func() {
			m.mu.Lock()
			defer m.mu.Unlock()
			if out, err := git(m.Dir, "push", "-q", "origin", "HEAD"); err != nil {
				git(m.Dir, "pull", "-q", "--rebase", "--autostash")
				if out2, err := git(m.Dir, "push", "-q", "origin", "HEAD"); err != nil {
					log.Printf("协作记忆 push 失败: %s / %s", out, out2)
				}
			}
		}()
	}
}

// routes reads routes.yml: "key: value" lines, # comments.
func (m *Memory) routes() map[string]string {
	raw, _ := os.ReadFile(filepath.Join(m.Dir, routesFile))
	return parseFlatYAML(string(raw))
}

// Route is the project dir for a workspace, "" when none.
func (m *Memory) Route(wsID string) string {
	if m == nil {
		return ""
	}
	d := m.routes()[wsID]
	if !projectNameRe.MatchString(d) {
		return ""
	}
	return d
}

func (m *Memory) SetRoute(wsID, dir string) error {
	dir = strings.TrimSpace(dir)
	if dir != "" && !projectNameRe.MatchString(dir) {
		return errors.New("记忆目录只能用字母, 数字, . _ -")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.routes()
	if dir == "" {
		delete(r, wsID)
	} else {
		r[wsID] = dir
		os.MkdirAll(filepath.Join(m.Dir, dir), 0o700)
	}
	keys := make([]string, 0, len(r))
	for k := range r {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("# 工作区 id: 记忆目录 (面板里配)\n")
	for _, k := range keys {
		b.WriteString(k + ": " + r[k] + "\n")
	}
	if err := os.WriteFile(filepath.Join(m.Dir, routesFile), []byte(b.String()), 0o600); err != nil {
		return err
	}
	m.commit("hxoad: 工作区 " + wsID + " -> " + dir)
	return nil
}

// Projects lists the project dirs present, for the panel's picker.
func (m *Memory) Projects() []string {
	out := []string{}
	entries, _ := os.ReadDir(m.Dir)
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			out = append(out, e.Name())
		}
	}
	return out
}

func (m *Memory) read(project, rel string) string {
	raw, err := os.ReadFile(filepath.Join(m.Dir, project, rel))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

func (m *Memory) appendLine(project, rel, line, msg string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := filepath.Join(m.Dir, project, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, err = f.WriteString(line + "\n")
	f.Close()
	if err != nil {
		return err
	}
	m.commit(msg)
	return nil
}

// Inbox lists the pending lessons ("- " lines of inbox.md) in file order.
func (m *Memory) Inbox(project string) []string {
	var out []string
	for _, l := range strings.Split(m.read(project, inboxFile), "\n") {
		if strings.HasPrefix(l, "- ") {
			out = append(out, strings.TrimPrefix(l, "- "))
		}
	}
	return out
}

// Resolve takes inbox item i out; adopt appends it to lessons.md or to an
// area file ("areas/client-web.md"), drop just removes it.
func (m *Memory) Resolve(project string, i int, adopt bool, to string) error {
	auto := to == ""
	if auto {
		to = lessonsFile
	}
	if to != lessonsFile && !(strings.HasPrefix(to, areasDir+"/") && strings.HasSuffix(to, ".md") && projectNameRe.MatchString(strings.TrimSuffix(strings.TrimPrefix(to, areasDir+"/"), ".md"))) {
		return errors.New("只能采纳进 lessons.md 或 areas/<名字>.md")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	p := filepath.Join(m.Dir, project, inboxFile)
	raw, _ := os.ReadFile(p)
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	n, item, keep := -1, "", []string{}
	for _, l := range lines {
		if strings.HasPrefix(l, "- ") {
			n++
			if n == i {
				item = strings.TrimPrefix(l, "- ")
				continue
			}
		}
		keep = append(keep, l)
	}
	if item == "" {
		return errors.New("收件箱里没有第 " + strconv.Itoa(i+1) + " 条")
	}
	if err := os.WriteFile(p, []byte(strings.Join(keep, "\n")+"\n"), 0o600); err != nil {
		return err
	}
	verb := "丢弃"
	if adopt && auto {
		// 只和某个目录有关的经验进那个目录的注意事项, brief 只留全局的
		if cs := claimsOf(item); len(cs) > 0 {
			ks := areaKeys(cs[0])
			to = areasDir + "/" + ks[len(ks)-1] + ".md"
		}
	}
	if adopt {
		dst := filepath.Join(m.Dir, project, to)
		os.MkdirAll(filepath.Dir(dst), 0o700)
		f, err := os.OpenFile(dst, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		f.WriteString("- " + item + "\n")
		f.Close()
		verb = "采纳进 " + to
	}
	m.commit("hxoad: " + project + " 经验" + verb + ": " + item)
	return nil
}

// Threads returns the last n message lines, newest last.
func (m *Memory) Threads(project string, n int) []string {
	dir := filepath.Join(m.Dir, project, threadsDir)
	entries, _ := os.ReadDir(dir)
	var lines []string
	for i := len(entries) - 1; i >= 0 && len(lines) < n; i-- {
		raw, _ := os.ReadFile(filepath.Join(dir, entries[i].Name()))
		day := strings.TrimSuffix(entries[i].Name(), ".md")
		var ls []string
		for _, l := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
			if strings.HasPrefix(l, "- ") {
				ls = append(ls, day+" "+strings.TrimPrefix(l, "- "))
			}
		}
		lines = append(ls, lines...)
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}

// areaKeys are the area file names a path may have notes under, shallow
// first: client/web/ui/x.tsx -> client, client-web, client-web-ui, ...
func areaKeys(rel string) []string {
	rel = strings.Trim(filepath.ToSlash(filepath.Clean("/"+rel)), "/")
	if rel == "" || rel == "." {
		return nil
	}
	parts := strings.Split(rel, "/")
	out := make([]string, 0, len(parts))
	for i := range parts {
		out = append(out, strings.Join(parts[:i+1], "-"))
	}
	return out
}

// parseFlatYAML reads "key: value" lines; enough for routes.yml.
func parseFlatYAML(s string) map[string]string {
	out := map[string]string{}
	for _, l := range strings.Split(s, "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		k, v, ok := strings.Cut(l, ":")
		if !ok {
			continue
		}
		out[strings.TrimSpace(k)] = unquote(strings.TrimSpace(v))
	}
	return out
}

func unquote(v string) string {
	if i := strings.Index(v, " #"); i >= 0 && !strings.HasPrefix(v, `"`) && !strings.HasPrefix(v, "'") {
		v = strings.TrimSpace(v[:i])
	}
	if len(v) >= 2 && (v[0] == '"' && v[len(v)-1] == '"' || v[0] == '\'' && v[len(v)-1] == '\'') {
		return v[1 : len(v)-1]
	}
	return v
}
