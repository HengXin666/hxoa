package server

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Memory entries are the "- " lines of brief.md, lessons.md, areas/*.md and
// inbox.md. Each gets a short id from its own text, so an agent can fix or
// drop the one it found wrong ("hxoa learn --fix m3f9a2c '...'") without the
// user reviewing first; every change is a commit the user can revert.

const (
	idPrefix      = "m"
	briefLessons  = 15 // lessons shown in the brief; the rest via recall
	recallMax     = 30
	fixMark       = "hxoa learn --"
	agentEditsMax = 20
)

// entryID is stable for the same text.
func entryID(text string) string {
	h := sha1.Sum([]byte(strings.TrimSpace(text)))
	return idPrefix + hex.EncodeToString(h[:])[:6]
}

type entry struct {
	File string // relative to the project dir
	Text string // without the leading "- "
	ID   string
}

// entryFiles are where entries live, inbox last.
func (m *Memory) entryFiles(project string) []string {
	out := []string{briefFile, lessonsFile}
	areas, _ := os.ReadDir(filepath.Join(m.Dir, project, areasDir))
	for _, a := range areas {
		if strings.HasSuffix(a.Name(), ".md") {
			out = append(out, filepath.ToSlash(filepath.Join(areasDir, a.Name())))
		}
	}
	return append(out, inboxFile)
}

func (m *Memory) entries(project string) []entry {
	var out []entry
	for _, f := range m.entryFiles(project) {
		for _, l := range strings.Split(m.read(project, f), "\n") {
			if strings.HasPrefix(l, "- ") {
				t := strings.TrimPrefix(l, "- ")
				out = append(out, entry{File: f, Text: t, ID: entryID(t)})
			}
		}
	}
	return out
}

// withIDs prefixes each "- " line with its id, for showing to agents.
func withIDs(body string) string {
	lines := strings.Split(body, "\n")
	for i, l := range lines {
		if strings.HasPrefix(l, "- ") {
			t := strings.TrimPrefix(l, "- ")
			lines[i] = "- [" + entryID(t) + "] " + t
		}
	}
	return strings.Join(lines, "\n")
}

// Edit replaces (text != "") or removes the entry with this id, recording
// who and why in the commit so the user can review and revert later.
func (m *Memory) Edit(project, id, text, who, why string) (string, error) {
	id = strings.TrimPrefix(strings.TrimSpace(id), "#")
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, f := range m.entryFiles(project) {
		p := filepath.Join(m.Dir, project, f)
		raw, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		lines := strings.Split(string(raw), "\n")
		for i, l := range lines {
			if !strings.HasPrefix(l, "- ") || entryID(strings.TrimPrefix(l, "- ")) != id {
				continue
			}
			old := strings.TrimPrefix(l, "- ")
			verb := "drop"
			if text != "" {
				lines[i] = "- " + text
				verb = "fix"
			} else {
				lines = append(lines[:i], lines[i+1:]...)
			}
			if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")), 0o600); err != nil {
				return "", err
			}
			msg := fmt.Sprintf("%s%s %s (%s) by %s: %s\n\n原文: %s", fixMark, verb, id, f, who, oneLine(why), old)
			if text != "" {
				msg += "\n新文: " + text
			}
			m.commit(msg)
			return f, nil
		}
	}
	return "", errors.New("记忆里没有编号 " + id + " (可能已被改过, 用 hxoa recall 重新查)")
}

// AgentEdits lists recent fixes and drops made by agents, newest first.
type AgentEdit struct {
	SHA     string `json:"sha"`
	Subject string `json:"subject"`
	At      string `json:"at"`
}

func (m *Memory) AgentEdits(project string) []AgentEdit {
	out, err := git(m.Dir, "log", "-n", "200", "--format=%h%x1f%s%x1f%cr", "--", project)
	if err != nil {
		return nil
	}
	var list []AgentEdit
	for _, l := range strings.Split(out, "\n") {
		p := strings.Split(l, "\x1f")
		if len(p) == 3 && strings.HasPrefix(p[1], fixMark) {
			list = append(list, AgentEdit{SHA: p[0], Subject: strings.TrimPrefix(p[1], fixMark), At: p[2]})
			if len(list) >= agentEditsMax {
				break
			}
		}
	}
	return list
}

var shaRe = regexp.MustCompile(`^[0-9a-f]{7,40}$`)

// Revert undoes one agent edit.
func (m *Memory) Revert(sha string) error {
	if !shaRe.MatchString(sha) {
		return errors.New("提交号不对")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if out, err := git(m.Dir, "-c", "user.name=hxoad", "-c", "user.email=hxoad@localhost", "revert", "--no-edit", sha); err != nil {
		git(m.Dir, "revert", "--abort")
		return errors.New("撤回失败 (之后又改过同一行?): " + out)
	}
	m.pushAsync()
	return nil
}

// pathRefRe finds path-like words (a/b, a/b.ts) an entry points at.
var pathRefRe = regexp.MustCompile("[A-Za-z0-9_.@-]+(?:/[A-Za-z0-9_.@-]+)+")

// staleRefs are paths an entry names whose first segment is a top-level
// dir of the tree but which no longer exist there.
func staleRefs(root, text string) []string {
	var out []string
	for _, p := range pathRefRe.FindAllString(text, -1) {
		p = strings.TrimRight(p, ".")
		if strings.Contains(p, "://") || strings.HasPrefix(p, "@") {
			continue
		}
		first := strings.SplitN(p, "/", 2)[0]
		if st, err := os.Stat(filepath.Join(root, first)); err != nil || !st.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, p)); err != nil {
			out = append(out, p)
		}
	}
	return out
}

// annotate marks entries that point at missing paths, so the agent checks
// and fixes them instead of trusting them.
func annotate(root, body string) string {
	lines := strings.Split(body, "\n")
	for i, l := range lines {
		if !strings.HasPrefix(l, "- ") || root == "" {
			continue
		}
		if gone := staleRefs(root, l); len(gone) > 0 {
			lines[i] = l + "  (疑似过时: " + strings.Join(gone, ", ") + " 已不存在, 核实后 hxoa learn --fix/--drop)"
		}
	}
	return strings.Join(lines, "\n")
}

// claimsOf reads "[a, b]" right before the colon of an inbox line.
var claimsRe = regexp.MustCompile(`\[([^\]]+)\]:`)

func claimsOf(item string) []string {
	m := claimsRe.FindStringSubmatch(item)
	if m == nil {
		return nil
	}
	var out []string
	for _, p := range strings.Split(m[1], ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// pendingFor are inbox items whose claimed dirs cover rel (not yet reviewed).
func (m *Memory) pendingFor(project, rel string) []string {
	var out []string
	for _, it := range m.Inbox(project) {
		for _, c := range claimsOf(it) {
			if under(rel, c) {
				out = append(out, "[未审] "+it)
				break
			}
		}
	}
	return out
}

// Recall finds entries and messages containing every keyword.
func (m *Memory) Recall(project string, words []string) []string {
	var out []string
	match := func(s string) bool {
		ls := strings.ToLower(s)
		for _, w := range words {
			if !strings.Contains(ls, strings.ToLower(w)) {
				return false
			}
		}
		return true
	}
	for _, e := range m.entries(project) {
		if match(e.Text) {
			tag := e.File
			if e.File == inboxFile {
				tag += " 未审"
			}
			out = append(out, fmt.Sprintf("[%s] %s: %s", e.ID, tag, e.Text))
		}
	}
	threads := m.Threads(project, 500)
	for i := len(threads) - 1; i >= 0; i-- {
		if match(threads[i]) {
			out = append(out, "threads: "+threads[i])
		}
	}
	if len(out) > recallMax {
		out = append(out[:recallMax], fmt.Sprintf("... 还有 %d 条, 多加关键词缩小范围", len(out)-recallMax))
	}
	return out
}
