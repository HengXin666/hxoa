package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/HengXin666/hxoa/internal/proto"
)

// Task is what a connection says it is working on: issue numbers plus a
// short note. The agent sets it with `hxoa task`, the user can set it from the
// panel when handing out work. It lives on the token, so deleting the
// connection drops it.
type Task struct {
	Issues []int     `json:"issues,omitempty"`
	Note   string    `json:"note,omitempty"`
	By     string    `json:"by,omitempty"` // "agent" or "user"
	At     time.Time `json:"at"`
}

const (
	maxTaskIssues = 20
	maxTaskNote   = 200
)

var issueRefRe = regexp.MustCompile(`(?:^|[^\w/&])#(\d{1,7})\b`)

// issueRefs pulls "#123" references out of commit messages, in order of
// first appearance.
func issueRefs(text string) []int {
	seen := map[int]bool{}
	var out []int
	for _, m := range issueRefRe.FindAllStringSubmatch(text, -1) {
		n, err := strconv.Atoi(m[1])
		if err != nil || n == 0 || seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	return out
}

// workerRefs lists the issues mentioned by commits only this worker carries:
// not on the user's current branch and not pushed anywhere. Workers that
// merge origin/main pick up everyone's pushed commits, so --remotes keeps
// those from showing up under every worker.
func workerRefs(ws *Workspace, branch string) []int {
	out, err := git(ws.Path, "log", "--format=%B", branch, "--not", "HEAD", "--remotes")
	if err != nil {
		return nil
	}
	return issueRefs(out)
}

var ghRemoteRe = regexp.MustCompile(`github\.com[:/]([\w.-]+)/([\w.-]+?)(?:\.git)?/?$`)

// repoURL is the GitHub page of the workspace's origin, or "" when origin is
// not on GitHub. The panel links issue numbers to it.
func repoURL(path string) string {
	out, err := git(path, "remote", "get-url", "origin")
	if err != nil {
		return ""
	}
	return githubURL(out)
}

func githubURL(remote string) string {
	m := ghRemoteRe.FindStringSubmatch(strings.TrimSpace(remote))
	if m == nil {
		return ""
	}
	return "https://github.com/" + m[1] + "/" + m[2]
}

// cleanTask validates input; nil means "clear".
func cleanTask(issues []int, note, by string) (*Task, error) {
	note = strings.TrimSpace(note)
	if len([]rune(note)) > maxTaskNote {
		return nil, errors.New("说明最长 " + strconv.Itoa(maxTaskNote) + " 字")
	}
	seen := map[int]bool{}
	var list []int
	for _, n := range issues {
		if n <= 0 {
			return nil, errors.New("issue 号要是正整数")
		}
		if !seen[n] {
			seen[n] = true
			list = append(list, n)
		}
	}
	if len(list) > maxTaskIssues {
		return nil, errors.New("一次最多 " + strconv.Itoa(maxTaskIssues) + " 个 issue")
	}
	if len(list) == 0 && note == "" {
		return nil, nil
	}
	sort.Ints(list)
	return &Task{Issues: list, Note: note, By: by, At: time.Now()}, nil
}

// SetTask replaces a connection's task; nil clears it.
func (s *Store) SetTask(id string, t *Task) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, tok := range s.tokens {
		if tok.ID == id {
			tok.Task = t
			s.dirty = true
			s.saveLocked()
			return true
		}
	}
	return false
}

// taskText is a one-line form for the op log and the CLI: "#104 #108 说明".
func taskText(t *Task) string {
	if t == nil {
		return "清空"
	}
	var parts []string
	for _, n := range t.Issues {
		parts = append(parts, fmt.Sprintf("#%d", n))
	}
	if t.Note != "" {
		parts = append(parts, t.Note)
	}
	return strings.Join(parts, " ")
}

func (t *Task) wire() *proto.Task {
	if t == nil {
		return nil
	}
	return &proto.Task{Issues: t.Issues, Note: t.Note, By: t.By, At: t.At.Format(time.RFC3339)}
}

// IssueInfo is an issue's title and state, read with the host's own `gh`
// login when it has one. Without gh the panel asks GitHub from the browser.
type IssueInfo struct {
	Title string `json:"title"`
	State string `json:"state"`
}

const issueCacheFor = 5 * time.Minute

var issueCache struct {
	sync.Mutex
	m map[string]issueCacheEntry
}

type issueCacheEntry struct {
	at   time.Time
	list map[int]IssueInfo
}

func repoIssues(repo string) map[int]IssueInfo {
	slug := strings.TrimPrefix(repo, "https://github.com/")
	if slug == repo || slug == "" {
		return nil
	}
	issueCache.Lock()
	defer issueCache.Unlock()
	if e, ok := issueCache.m[slug]; ok && time.Since(e.at) < issueCacheFor {
		return e.list
	}
	out, err := exec.Command("gh", "issue", "list", "-R", slug, "-s", "all", "-L", "500", "--json", "number,title,state").Output()
	list := map[int]IssueInfo{}
	if err == nil {
		var rows []struct {
			Number int    `json:"number"`
			Title  string `json:"title"`
			State  string `json:"state"`
		}
		if json.Unmarshal(out, &rows) == nil {
			for _, r := range rows {
				list[r.Number] = IssueInfo{Title: r.Title, State: strings.ToLower(r.State)}
			}
		}
	}
	if issueCache.m == nil {
		issueCache.m = map[string]issueCacheEntry{}
	}
	issueCache.m[slug] = issueCacheEntry{at: time.Now(), list: list}
	return list
}
