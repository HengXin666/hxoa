package server

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/HengXin666/hxoa/internal/proto"
)

// Collaboration between connections of one workspace rides on the calls
// agents already make: messages, directory notes and overlap warnings are
// queued per connection and delivered in the NoticeHeader of its next
// response, so nobody polls and nothing is missed.

const (
	maxSay        = 500
	maxLearn      = 300
	maxClaims     = 20
	overlapRepeat = 30 * time.Minute
	threadTail    = 40
)

type collab struct {
	mu     sync.Mutex
	queue  map[string][]string        // token id -> pending notices
	areas  map[string]map[string]bool // token id -> area keys already shown
	warned map[string]time.Time       // token|other|claim -> last warning
	brief  map[string]bool            // token id -> has seen the brief
}

// briefed marks a connection as having seen the brief; reports whether it
// already had.
func (c *collab) briefed(tokID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.brief == nil {
		c.brief = map[string]bool{}
	}
	had := c.brief[tokID]
	c.brief[tokID] = true
	return had
}

func (c *collab) push(tokID, msg string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.queue == nil {
		c.queue = map[string][]string{}
	}
	if len(c.queue[tokID]) < 50 {
		c.queue[tokID] = append(c.queue[tokID], msg)
	}
}

func (c *collab) drain(tokID string) []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := c.queue[tokID]
	delete(c.queue, tokID)
	return out
}

// firstTouch reports whether this connection sees the area for the first time.
func (c *collab) firstTouch(tokID, key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.areas == nil {
		c.areas = map[string]map[string]bool{}
	}
	if c.areas[tokID] == nil {
		c.areas[tokID] = map[string]bool{}
	}
	if c.areas[tokID][key] {
		return false
	}
	c.areas[tokID][key] = true
	return true
}

func (c *collab) shouldWarn(key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.warned == nil {
		c.warned = map[string]time.Time{}
	}
	if time.Since(c.warned[key]) < overlapRepeat {
		return false
	}
	c.warned[key] = time.Now()
	return true
}

// cleanClaims normalizes claimed directories: relative, slash separated, no "..".
func cleanClaims(paths []string) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	for _, a := range paths {
		for _, p := range strings.FieldsFunc(a, func(r rune) bool { return r == ',' || r == ' ' }) {
			p = strings.Trim(filepath.ToSlash(filepath.Clean("/"+p)), "/")
			if p == "" || strings.HasPrefix(p, "..") {
				return nil, fmt.Errorf("认领的目录要是工作区里的相对路径: %s", a)
			}
			if !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	if len(out) > maxClaims {
		return nil, fmt.Errorf("一次最多认领 %d 个目录", maxClaims)
	}
	return out, nil
}

func under(rel, dir string) bool { return rel == dir || strings.HasPrefix(rel, dir+"/") }

// peekPaths reads the workspace paths a request names (query "path", body
// "path" / "from" / "to") without consuming the body.
func peekPaths(r *http.Request) []string {
	var out []string
	if p := r.URL.Query().Get("path"); p != "" {
		out = append(out, p)
	}
	if r.Body == nil || r.Method == http.MethodGet {
		return out
	}
	raw, err := io.ReadAll(r.Body)
	r.Body = io.NopCloser(bytes.NewReader(raw))
	if err != nil {
		return out
	}
	var v struct {
		Path string `json:"path"`
		From string `json:"from"`
		To   string `json:"to"`
	}
	if json.Unmarshal(raw, &v) == nil {
		for _, p := range []string{v.Path, v.From, v.To} {
			if p != "" {
				out = append(out, p)
			}
		}
	}
	return out
}

var writeOps = map[string]bool{"write": true, "patch": true, "rm": true, "mv": true, "mkdir": true}

// others are the other connections of the same workspace.
func (s *Server) others(c *reqCtx) []Token {
	_, ts := s.Store.Snapshot()
	var out []Token
	for _, t := range ts {
		if t.Workspace == c.ws.ID && t.ID != c.tok.ID {
			out = append(out, t)
		}
	}
	return out
}

// notices collects what this response should carry: the queue, notes of
// directories touched for the first time, and overlap warnings for writes.
func (s *Server) notices(c *reqCtx, op string, paths []string) []string {
	out := s.collab.drain(c.tok.ID)
	project := s.Memory.Route(c.ws.ID)
	// Agents that skip `hxoa task` still get the brief once, on their first
	// write or command, instead of being blocked.
	if project != "" && (writeOps[op] || op == "exec") && !s.collab.briefed(c.tok.ID) {
		out = append(out, "你还没看过本项目的 brief (开工应先 hxoa task <号> --paths <目录>), 先读完再继续:\n"+s.Brief(c.ws, c.tok, c.root))
	}
	for _, p := range paths {
		rel := strings.Trim(filepath.ToSlash(filepath.Clean("/"+p)), "/")
		if project != "" {
			for _, k := range areaKeys(rel) {
				if !s.collab.firstTouch(c.tok.ID, k) {
					continue
				}
				if note := s.Memory.read(project, filepath.Join(areasDir, k+".md")); note != "" {
					out = append(out, "注意事项 "+k+":\n"+withIDs(annotate(c.root, note)))
				}
			}
			for _, it := range s.Memory.pendingFor(project, rel) {
				if s.collab.firstTouch(c.tok.ID, "pending:"+entryID(it)) {
					out = append(out, it+" (还没经主人确认, 当参考)")
				}
			}
		}
		if !writeOps[op] {
			continue
		}
		for _, o := range s.others(c) {
			if o.Task == nil {
				continue
			}
			for _, claim := range o.Task.Paths {
				if under(rel, claim) && s.collab.shouldWarn(c.tok.ID+"|"+o.ID+"|"+claim) {
					out = append(out, fmt.Sprintf("%s 认领了 %s%s, 你在改 %s; 改完用 hxoa say '做了什么, 影响谁' 告诉它", o.Label, claim, issuesText(o.Task), rel))
				}
			}
		}
	}
	return out
}

func setNotices(w http.ResponseWriter, list []string) {
	if len(list) == 0 {
		return
	}
	raw, _ := json.Marshal(list)
	w.Header().Set(proto.NoticeHeader, base64.StdEncoding.EncodeToString(raw))
}

func issuesText(t *Task) string {
	if t == nil || len(t.Issues) == 0 {
		return ""
	}
	var parts []string
	for _, n := range t.Issues {
		parts = append(parts, fmt.Sprintf("#%d", n))
	}
	return " (" + strings.Join(parts, " ") + ")"
}

func claimText(t *Task) string {
	if t == nil {
		return "还没登记任务"
	}
	s := strings.TrimSpace(strings.TrimPrefix(issuesText(t), " "))
	s = strings.TrimSuffix(strings.TrimPrefix(s, "("), ")")
	if s == "" {
		s = "没写 issue"
	}
	if len(t.Paths) > 0 {
		s += " 认领 " + strings.Join(t.Paths, ", ")
	}
	return s
}

// Brief is the start-of-work pack for a connection.
func (s *Server) Brief(ws *Workspace, tok *Token, root string) string {
	project := s.Memory.Route(ws.ID)
	var b strings.Builder
	head := ws.Name
	if project != "" {
		head = project
	}
	fmt.Fprintf(&b, "# %s · 你是 %s · %s\n", head, tok.Label, claimText(tok.Task))
	brief := ""
	if project != "" {
		brief = s.Memory.read(project, briefFile)
	}
	if brief == "" {
		b.WriteString("这个工作区还没有 brief (主人在面板 → 工作区 → 协作记忆里配)。先读仓库的 AGENTS.md 与 README。\n")
	} else {
		fmt.Fprintf(&b, "先读下面 brief (%d 行), 碰到新目录时会自动补充注意事项。\n\n%s\n", strings.Count(brief, "\n")+1, withIDs(annotate(root, brief)))
	}
	if lessons := s.Memory.readIf(project, lessonsFile); lessons != "" {
		ls := strings.Split(lessons, "\n")
		more := ""
		if len(ls) > briefLessons {
			more = fmt.Sprintf("\n(还有 %d 条较早的, 用 hxoa recall <关键词> 查)", len(ls)-briefLessons)
			ls = ls[len(ls)-briefLessons:]
		}
		b.WriteString("\n## 主人确认过的经验\n" + withIDs(annotate(root, strings.Join(ls, "\n"))) + more + "\n")
	}
	_, ts := s.Store.Snapshot()
	var others []string
	for _, t := range ts {
		if t.Workspace == ws.ID && t.ID != tok.ID && t.Task != nil {
			others = append(others, t.Label+" "+claimText(t.Task))
		}
	}
	if len(others) == 0 {
		b.WriteString("\n其他 worker: 没有登记任务的\n")
	} else {
		b.WriteString("\n其他 worker: " + strings.Join(others, "; ") + "\n")
	}
	b.WriteString("你只需要记住:\n")
	b.WriteString("- 开工: hxoa task <号> --paths <要改的目录> (认领后别人改到会提醒他告诉你)\n")
	b.WriteString("- 改到别人认领的目录或共用文件 (协议, 常量): hxoa say '做了什么, 影响谁'\n")
	b.WriteString("- 踩坑或被主人打回: hxoa learn '一句话规则'\n")
	b.WriteString("- 记忆和代码或仓库文档冲突时以后者为准, 顺手改掉: hxoa learn --fix <编号> '新说法' 或 --drop <编号> '原因', 直接生效\n")
	b.WriteString("- 按需查更多: hxoa recall <关键词>\n")
	if project != "" && len(parseChecks(s.Memory.read(project, checksFile))) > 0 {
		b.WriteString("- commit 会自动跑 checks.yml, 不过就拒\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m *Memory) readIf(project, rel string) string {
	if m == nil || project == "" {
		return ""
	}
	return m.read(project, rel)
}

// Say queues a message for the other connections (or the one labelled to)
// and logs it to the project's threads.
func (s *Server) Say(c *reqCtx, text, to string) ([]string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, errf(proto.CodeBadRequest, "说点什么: hxoa say '做了什么, 影响谁'")
	}
	if len([]rune(text)) > maxSay {
		return nil, errf(proto.CodeBadRequest, fmt.Sprintf("一句话就好, 最多 %d 字", maxSay))
	}
	var sent []string
	for _, o := range s.others(c) {
		if to != "" && o.Label != to {
			continue
		}
		s.collab.push(o.ID, fmt.Sprintf("%s%s 说: %s", c.tok.Label, issuesText(c.tok.Task), text))
		sent = append(sent, o.Label)
	}
	if to != "" && len(sent) == 0 {
		return nil, errf(proto.CodeNotFound, "这个工作区没有叫 "+to+" 的连接")
	}
	if project := s.Memory.Route(c.ws.ID); project != "" {
		now := time.Now()
		target := "所有人"
		if to != "" {
			target = to
		}
		line := fmt.Sprintf("- %s %s%s -> %s: %s", now.Format("15:04"), c.tok.Label, issuesText(c.tok.Task), target, oneLine(text))
		s.Memory.appendLine(project, filepath.Join(threadsDir, now.Format("2006-01-02")+".md"), line, "hxoa say: "+c.tok.Label)
	}
	return sent, nil
}

// Learn files a lesson into the project's inbox for the user to review.
func (s *Server) Learn(c *reqCtx, text string) error {
	text = oneLine(strings.TrimSpace(text))
	if text == "" {
		return errf(proto.CodeBadRequest, "写一句能判断对错的规则: hxoa learn '插件的界面只进插件空间'")
	}
	if len([]rune(text)) > maxLearn {
		return errf(proto.CodeBadRequest, fmt.Sprintf("一句话就好, 最多 %d 字", maxLearn))
	}
	project := s.Memory.Route(c.ws.ID)
	if project == "" {
		return errf(proto.CodeDenied, "这个工作区还没接协作记忆, 请主人在面板里配")
	}
	claims := ""
	if c.tok.Task != nil && len(c.tok.Task.Paths) > 0 {
		claims = " [" + strings.Join(c.tok.Task.Paths, ", ") + "]"
	}
	line := fmt.Sprintf("- %s %s%s%s: %s", time.Now().Format("2006-01-02"), c.tok.Label, issuesText(c.tok.Task), claims, text)
	return s.Memory.appendLine(project, inboxFile, line, "hxoa learn: "+c.tok.Label)
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// EditEntry is an agent fixing (text != "") or dropping a memory entry it
// found wrong in practice; takes effect at once, the user reviews later.
func (s *Server) EditEntry(c *reqCtx, id, text, why string) (string, error) {
	project := s.Memory.Route(c.ws.ID)
	if project == "" {
		return "", errf(proto.CodeDenied, "这个工作区还没接协作记忆")
	}
	text, why = oneLine(text), oneLine(why)
	if text == "" && why == "" {
		return "", errf(proto.CodeBadRequest, "删掉要写原因: hxoa learn --drop <编号> '原因'")
	}
	if len([]rune(text)) > maxLearn {
		return "", errf(proto.CodeBadRequest, fmt.Sprintf("一句话就好, 最多 %d 字", maxLearn))
	}
	if text != "" {
		text = fmt.Sprintf("%s (%s %s 改)", text, time.Now().Format("2006-01-02"), c.tok.Label)
	}
	f, err := s.Memory.Edit(project, id, text, c.tok.Label, why)
	if err != nil {
		return "", errf(proto.CodeNotFound, err.Error())
	}
	return f, nil
}
