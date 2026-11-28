// Command hxoa is the agent-side client: it reads, edits and runs commands in
// a workspace shared by hxoad on the user's machine.
//
// Output is kept terse on purpose (it is read by a model): metadata goes to
// stderr as one "# ..." line, content to stdout. Exit codes: 0 ok, 1 error,
// 2 usage, 3 conflict (re-read and retry), 4 not found.
package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/HengXin666/hxoa/internal/proto"
)

//go:embed guide.md
var guide string

type config struct {
	Base  string `json:"base"`
	Token string `json:"token"`
	// Versions remembers the version of every file this client last read or
	// wrote, so edits carry the right guard without the model copying hashes.
	Versions map[string]string `json:"versions,omitempty"`
}

// cfgPath: HXOA_CONFIG, else ~/.hxoa/<HXOA_PROFILE or "config">.json, so one
// agent can hold several workspaces side by side.
func cfgPath() string {
	if p := os.Getenv("HXOA_CONFIG"); p != "" {
		return p
	}
	name := "config"
	if p := os.Getenv("HXOA_PROFILE"); p != "" {
		name = p
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".hxoa", name+".json")
}

func key(p string) string {
	return strings.TrimPrefix(filepath.ToSlash(filepath.Clean("/"+p)), "/")
}

// remember stores (or with v=="" forgets) a path's version; best effort.
func remember(c *config, p, v string) {
	fresh := &config{}
	if raw, err := os.ReadFile(cfgPath()); err == nil {
		json.Unmarshal(raw, fresh)
	} else {
		return
	}
	if fresh.Versions == nil {
		fresh.Versions = map[string]string{}
	}
	if v == "" {
		delete(fresh.Versions, key(p))
	} else {
		fresh.Versions[key(p)] = v
	}
	if len(fresh.Versions) > 2000 {
		fresh.Versions = map[string]string{key(p): v}
	}
	c.Versions = fresh.Versions
	saveCfg(fresh)
}

func loadCfg() (*config, error) {
	c := &config{}
	if raw, err := os.ReadFile(cfgPath()); err == nil {
		json.Unmarshal(raw, c)
	}
	if v := os.Getenv("HXOA_BASE"); v != "" {
		c.Base = v
	}
	if v := os.Getenv("HXOA_TOKEN"); v != "" {
		c.Token = v
	}
	if c.Base == "" || c.Token == "" {
		return nil, errors.New("还没连接, 先运行: hxoa login <token> --base <url>")
	}
	return c, nil
}

func saveCfg(c *config) error {
	p := cfgPath()
	os.MkdirAll(filepath.Dir(p), 0o700)
	raw, _ := json.MarshalIndent(c, "", "  ")
	tmp := fmt.Sprintf("%s.%d", p, os.Getpid())
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

type exitErr struct {
	code int
	msg  string
}

func (e *exitErr) Error() string { return e.msg }

func usage(msg string) error { return &exitErr{2, msg} }

// call does one request and decodes into out; protocol errors become exitErr.
func call(c *config, method, path string, q url.Values, body any, out any) error {
	u := strings.TrimRight(c.Base, "/") + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, u, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("User-Agent", "hxoa/2")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	timeout := 200 * time.Second
	if t := os.Getenv("HXOA_HTTP_TIMEOUT"); t != "" {
		if n, err := strconv.Atoi(t); err == nil {
			timeout = time.Duration(n) * time.Second
		}
	}
	if path == "/v1/exec" {
		if m, ok := body.(*proto.ExecReq); ok && m.Timeout > 0 {
			timeout = time.Duration(m.Timeout+30) * time.Second
		}
	}
	resp, err := (&http.Client{Timeout: timeout}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	printNotices(resp.Header.Get(proto.NoticeHeader))
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		var pe proto.Error
		if json.Unmarshal(raw, &pe) != nil || pe.Error == "" {
			return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
		}
		msg := pe.Code + ": " + pe.Error
		if pe.Current != "" {
			msg += " (当前 v=" + pe.Current + ")"
		}
		switch pe.Code {
		case proto.CodeConflict:
			if m, ok := body.(interface{ Target() string }); ok {
				remember(c, m.Target(), "")
			}
			return &exitErr{3, msg + "; 请重新 cat 再改"}
		case proto.CodeExists, proto.CodeAmbiguous, proto.CodeNoMatch:
			return &exitErr{3, msg}
		case proto.CodeNotFound:
			return &exitErr{4, msg}
		}
		return &exitErr{1, msg}
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}

// flags is a tiny parser: known flags may appear anywhere; "--" ends them.
type flags struct {
	pos  []string
	vals map[string]string
	set  map[string]bool
}

func parse(args []string, withValue ...string) *flags {
	f := &flags{vals: map[string]string{}, set: map[string]bool{}}
	takes := map[string]bool{}
	for _, w := range withValue {
		takes[w] = true
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			f.pos = append(f.pos, args[i+1:]...)
			break
		}
		if strings.HasPrefix(a, "-") && len(a) > 1 {
			name := strings.TrimLeft(a, "-")
			if k, v, ok := strings.Cut(name, "="); ok {
				f.vals[k], f.set[k] = v, true
				continue
			}
			if takes[name] && i+1 < len(args) {
				f.vals[name], f.set[name] = args[i+1], true
				i++
				continue
			}
			f.set[name] = true
			continue
		}
		f.pos = append(f.pos, a)
	}
	return f
}

func (f *flags) str(names ...string) string {
	for _, n := range names {
		if v, ok := f.vals[n]; ok {
			return v
		}
	}
	return ""
}

func (f *flags) has(names ...string) bool {
	for _, n := range names {
		if f.set[n] {
			return true
		}
	}
	return false
}

func (f *flags) num(def int, names ...string) int {
	if v := f.str(names...); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func meta(format string, a ...any) { fmt.Fprintf(os.Stderr, "# "+format+"\n", a...) }

func readStdin() (string, error) {
	b, err := io.ReadAll(os.Stdin)
	return string(b), err
}

func main() {
	err := run(os.Args[1:])
	if err == nil {
		return
	}
	code := 1
	var ee *exitErr
	if errors.As(err, &ee) {
		code = ee.code
	}
	fmt.Fprintln(os.Stderr, "hxoa: "+err.Error())
	os.Exit(code)
}

func run(args []string) error {
	if len(args) == 0 {
		fmt.Print(guide)
		return nil
	}
	// Accept the older one-line form: hxoa --base URL --login TOKEN
	if strings.HasPrefix(args[0], "-") {
		f := parse(args, "base", "login")
		if f.has("login") {
			return cmdLogin(f.str("login"), f.str("base"))
		}
		if f.has("h", "help") {
			fmt.Print(guide)
			return nil
		}
		return usage("未知参数 " + args[0])
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "help", "guide":
		fmt.Print(guide)
		return nil
	case "version":
		fmt.Println("hxoa 2 (proto", proto.Version, ")")
		return nil
	case "login":
		f := parse(rest, "base")
		if len(f.pos) < 1 {
			return usage("login <token> --base <url>")
		}
		return cmdLogin(f.pos[0], f.str("base"))
	}
	c, err := loadCfg()
	if err != nil {
		return err
	}
	switch cmd {
	case "status", "hello", "whoami":
		return cmdHello(c)
	case "logout":
		call(c, "POST", "/v1/bye", nil, nil, nil)
		os.Remove(cfgPath())
		fmt.Println("已断开")
		return nil
	case "ls":
		return cmdLs(c, rest)
	case "cat", "read":
		return cmdCat(c, rest)
	case "stat", "ver":
		return cmdStat(c, rest)
	case "write", "put":
		return cmdWrite(c, rest)
	case "push":
		return cmdPush(c, rest)
	case "patch":
		return cmdPatch(c, rest)
	case "replace":
		return cmdReplace(c, rest)
	case "rm":
		return cmdRm(c, rest)
	case "mv":
		return cmdMv(c, rest)
	case "mkdir":
		return cmdMkdir(c, rest)
	case "grep":
		return cmdGrep(c, rest)
	case "exec", "run":
		return cmdExec(c, rest)
	case "git":
		var pre, gitArgs []string
		for _, a := range rest {
			if a == "--skip-check" {
				pre = append(pre, a)
			} else {
				gitArgs = append(gitArgs, a)
			}
		}
		return cmdExec(c, append(append(pre, "--", "git"), quoteAll(gitArgs)...))
	case "job":
		return cmdJob(c, rest)
	case "pull":
		return cmdPull(c, rest)
	case "task":
		return cmdTask(c, rest)
	case "brief":
		return cmdBrief(c)
	case "say":
		return cmdSay(c, rest)
	case "learn":
		return cmdLearn(c, rest)
	case "merge":
		return cmdMerge(c)
	}
	return usage("未知命令 " + cmd + ", 运行 hxoa guide 查看用法")
}

func cmdLogin(token, base string) error {
	if base == "" {
		base = os.Getenv("HXOA_BASE")
	}
	if token == "" || base == "" {
		return usage("login <token> --base <url>")
	}
	c := &config{Base: strings.TrimRight(base, "/"), Token: token}
	var h proto.Hello
	if err := call(c, "GET", "/v1/hello", nil, nil, &h); err != nil {
		return err
	}
	if err := saveCfg(c); err != nil {
		return err
	}
	printHello(&h)
	return nil
}

func printHello(h *proto.Hello) {
	fmt.Printf("已连接 %s, 身份 %s, 模式 %s", h.Workspace, h.Identity, h.Mode)
	if h.Mode == "worker" {
		fmt.Printf(" (分支 %s, 基于 %s)", h.Branch, h.Base)
	}
	if !h.Exec {
		fmt.Print(", 命令执行已关闭")
	}
	fmt.Printf("\n根目录 %s\n", h.Root)
	if h.Task != nil {
		printTask(h.Task)
	}
	fmt.Println("首次使用先跑 hxoa guide 看规则 (写入前必须先读, 冲突重读再改)")
	fmt.Println("开工先 hxoa task <号> --paths <要改的目录>: 登记认领并打印本项目的 brief")
	if h.Proto != proto.Version {
		fmt.Printf("注意: 服务端协议 %d, 客户端 %d, 请更新\n", h.Proto, proto.Version)
	}
}

func cmdHello(c *config) error {
	var h proto.Hello
	if err := call(c, "GET", "/v1/hello", nil, nil, &h); err != nil {
		return err
	}
	printHello(&h)
	return nil
}

func cmdLs(c *config, args []string) error {
	f := parse(args, "d", "depth", "m", "max")
	q := url.Values{}
	if len(f.pos) > 0 {
		q.Set("path", f.pos[0])
	}
	q.Set("depth", strconv.Itoa(f.num(1, "d", "depth")))
	if m := f.num(0, "m", "max"); m > 0 {
		q.Set("max", strconv.Itoa(m))
	}
	var r proto.LsReply
	if err := call(c, "GET", "/v1/ls", q, nil, &r); err != nil {
		return err
	}
	var b strings.Builder
	for _, e := range r.Entries {
		if e.Dir {
			b.WriteString(e.Path + "/\n")
		} else {
			b.WriteString(e.Path + " " + human(e.Bytes) + "\n")
		}
	}
	fmt.Print(b.String())
	if r.Truncated {
		meta("结果已截断, 用更具体的路径或 -m")
	}
	return nil
}

func human(n int64) string {
	switch {
	case n < 1024:
		return strconv.FormatInt(n, 10)
	case n < 1<<20:
		return fmt.Sprintf("%.1fK", float64(n)/1024)
	}
	return fmt.Sprintf("%.1fM", float64(n)/(1<<20))
}

func read(c *config, path string, start, end int) (*proto.ReadReply, error) {
	q := url.Values{"path": {path}}
	if start > 0 {
		q.Set("start", strconv.Itoa(start))
	}
	if end > 0 {
		q.Set("end", strconv.Itoa(end))
	}
	var r proto.ReadReply
	if err := call(c, "GET", "/v1/read", q, nil, &r); err != nil {
		if ee, ok := err.(*exitErr); ok && ee.code == 4 {
			remember(c, path, "")
		}
		return &r, err
	}
	remember(c, path, r.Version)
	return &r, nil
}

func cmdCat(c *config, args []string) error {
	f := parse(args)
	if len(f.pos) < 1 {
		return usage("cat <path> [start] [end] [-n]")
	}
	start, end := 0, 0
	if len(f.pos) > 1 {
		start, _ = strconv.Atoi(f.pos[1])
	}
	if len(f.pos) > 2 {
		end, _ = strconv.Atoi(f.pos[2])
	}
	r, err := read(c, f.pos[0], start, end)
	if err != nil {
		return err
	}
	if r.Binary {
		meta("%s v=%s 二进制文件, 用 pull 下载", r.Path, r.Version)
		return nil
	}
	if r.Start > 0 {
		meta("%s v=%s lines %d-%d/%d rv=%s", r.Path, r.Version, r.Start, r.End, r.Lines, r.RangeVersion)
	} else {
		meta("%s v=%s lines=%d", r.Path, r.Version, r.Lines)
	}
	if f.has("n") {
		n := r.Start
		if n == 0 {
			n = 1
		}
		lines := strings.Split(strings.TrimSuffix(r.Content, "\n"), "\n")
		var b strings.Builder
		for i, l := range lines {
			if r.Content == "" {
				break
			}
			b.WriteString(strconv.Itoa(n+i) + "\t" + l + "\n")
		}
		fmt.Print(b.String())
		return nil
	}
	fmt.Print(r.Content)
	return nil
}

func cmdStat(c *config, args []string) error {
	if len(args) < 1 {
		return usage("stat <path>")
	}
	r, err := read(c, args[0], 1, 1)
	if err != nil {
		return err
	}
	fmt.Printf("%s v=%s lines=%d\n", r.Path, r.Version, r.Lines)
	return nil
}

// guard turns -v/--new/--force into an if_match value. With none of them it
// uses the version remembered from this client's last read of the path, so a
// write always means "on top of what I saw" and a file never read is refused.
func guard(c *config, path string, f *flags, allowNew bool) (string, error) {
	switch {
	case f.has("force", "f"):
		return "*", nil
	case allowNew && f.has("new"):
		return "new", nil
	case f.str("v", "version") != "":
		return f.str("v", "version"), nil
	}
	if v := c.Versions[key(path)]; v != "" {
		return v, nil
	}
	if allowNew {
		return "", usage(path + ": 还没读过这个文件; 先 hxoa cat 再改, 新建用 --new")
	}
	return "", usage(path + ": 还没读过这个文件; 先 hxoa cat 或 stat")
}

func printWrite(c *config, r *proto.WriteReply) {
	remember(c, r.Path, r.Version)
	s := "ok " + r.Path + " v=" + r.Version + " lines=" + strconv.Itoa(r.Lines)
	if r.Created {
		s += " (新建)"
	}
	fmt.Println(s)
}

func cmdWrite(c *config, args []string) error {
	f := parse(args, "v", "version")
	if len(f.pos) < 1 {
		return usage("write <path> (-v <version> | --new | --force) < content")
	}
	g, err := guard(c, f.pos[0], f, true)
	if err != nil {
		return err
	}
	body, err := readStdin()
	if err != nil {
		return err
	}
	var r proto.WriteReply
	if err := call(c, "POST", "/v1/write", nil, &proto.WriteReq{Path: f.pos[0], Content: body, IfMatch: g}, &r); err != nil {
		return err
	}
	printWrite(c, &r)
	return nil
}

func cmdPush(c *config, args []string) error {
	f := parse(args, "v", "version")
	if len(f.pos) < 2 {
		return usage("push <local-file> <remote-path> (-v <version> | --new | --force)")
	}
	g, err := guard(c, f.pos[1], f, true)
	if err != nil {
		return err
	}
	b, err := os.ReadFile(f.pos[0])
	if err != nil {
		return err
	}
	var r proto.WriteReply
	if err := call(c, "POST", "/v1/write", nil, &proto.WriteReq{Path: f.pos[1], Content: string(b), IfMatch: g}, &r); err != nil {
		return err
	}
	printWrite(c, &r)
	return nil
}

func cmdPatch(c *config, args []string) error {
	f := parse(args, "v", "version", "r", "range")
	if len(f.pos) < 3 {
		return usage("patch <path> <start> <end> (-v <version> | -r <range_version>) < new-lines")
	}
	start, e1 := strconv.Atoi(f.pos[1])
	end, e2 := strconv.Atoi(f.pos[2])
	if e1 != nil || e2 != nil {
		return usage("start/end 必须是数字")
	}
	req := &proto.PatchReq{Path: f.pos[0], IfMatch: f.str("v", "version"), RangeMatch: f.str("r", "range")}
	if len(f.pos) > 3 && req.IfMatch == "" && req.RangeMatch == "" {
		req.IfMatch = f.pos[3] // old positional form
	}
	if f.has("force") {
		cur, err := read(c, f.pos[0], 1, 1)
		if err != nil {
			return err
		}
		req.IfMatch = cur.Version
	}
	if req.IfMatch == "" && req.RangeMatch == "" {
		req.IfMatch = c.Versions[key(f.pos[0])]
	}
	if req.IfMatch == "" && req.RangeMatch == "" {
		return usage(f.pos[0] + ": 还没读过这个文件; 先 hxoa cat")
	}
	body, err := readStdin()
	if err != nil {
		return err
	}
	var lines []string
	if t := strings.TrimSuffix(body, "\n"); body != "" {
		lines = strings.Split(t, "\n")
	}
	req.Edits = []proto.Edit{{Op: "lines", Start: start, End: end, Lines: lines}}
	var r proto.WriteReply
	if err := call(c, "POST", "/v1/patch", nil, req, &r); err != nil {
		return err
	}
	printWrite(c, &r)
	return nil
}

// cmdReplace: either --old-file/--new-file (exact text from local files), or a
// JSON array of {"old","new","all"} on stdin for several edits at once.
func cmdReplace(c *config, args []string) error {
	f := parse(args, "v", "version", "old-file", "new-file", "old", "new")
	if len(f.pos) < 1 {
		return usage("replace <path> -v <version> (--old-file A --new-file B | --old S --new S | < edits.json)")
	}
	req := &proto.PatchReq{Path: f.pos[0], IfMatch: f.str("v", "version")}
	if req.IfMatch == "" {
		req.IfMatch = c.Versions[key(f.pos[0])]
	}
	if req.IfMatch == "" {
		return usage(f.pos[0] + ": 还没读过这个文件; 先 hxoa cat")
	}
	switch {
	case f.has("old-file"):
		o, err := os.ReadFile(f.str("old-file"))
		if err != nil {
			return err
		}
		n, err := os.ReadFile(f.str("new-file"))
		if err != nil && f.has("new-file") {
			return err
		}
		req.Edits = []proto.Edit{{Op: "replace", Old: string(o), New: string(n), All: f.has("all")}}
	case f.has("old"):
		req.Edits = []proto.Edit{{Op: "replace", Old: f.str("old"), New: f.str("new"), All: f.has("all")}}
	default:
		body, err := readStdin()
		if err != nil {
			return err
		}
		if err := json.Unmarshal([]byte(body), &req.Edits); err != nil {
			return usage("stdin 需要是 JSON 数组 [{\"old\":..,\"new\":..}]: " + err.Error())
		}
		for i := range req.Edits {
			req.Edits[i].Op = "replace"
		}
	}
	var r proto.WriteReply
	if err := call(c, "POST", "/v1/patch", nil, req, &r); err != nil {
		return err
	}
	printWrite(c, &r)
	return nil
}

func cmdRm(c *config, args []string) error {
	f := parse(args, "v", "version")
	if len(f.pos) < 1 {
		return usage("rm <path> (-v <version> | --force)")
	}
	g, err := guard(c, f.pos[0], f, false)
	if err != nil {
		return err
	}
	if err := call(c, "POST", "/v1/rm", nil, &proto.RmReq{Path: f.pos[0], IfMatch: g}, nil); err != nil {
		return err
	}
	remember(c, f.pos[0], "")
	fmt.Println("ok")
	return nil
}

func cmdMkdir(c *config, args []string) error {
	f := parse(args)
	if len(f.pos) < 1 {
		return usage("mkdir <path>")
	}
	var out struct {
		Created bool `json:"created"`
	}
	if err := call(c, "POST", "/v1/mkdir", nil, &proto.MkdirReq{Path: f.pos[0]}, &out); err != nil {
		return err
	}
	if out.Created {
		fmt.Println("ok")
	} else {
		fmt.Println("ok (已存在)")
	}
	return nil
}

func cmdMv(c *config, args []string) error {
	f := parse(args, "v", "version")
	if len(f.pos) < 2 {
		return usage("mv <from> <to> (-v <version> | --force)")
	}
	g, err := guard(c, f.pos[0], f, false)
	if err != nil {
		return err
	}
	if err := call(c, "POST", "/v1/mv", nil, &proto.MvReq{From: f.pos[0], To: f.pos[1], IfMatch: g}, nil); err != nil {
		return err
	}
	remember(c, f.pos[1], c.Versions[key(f.pos[0])])
	remember(c, f.pos[0], "")
	fmt.Println("ok")
	return nil
}

func cmdGrep(c *config, args []string) error {
	f := parse(args, "g", "glob", "m", "max")
	if len(f.pos) < 1 {
		return usage("grep <pattern> [path] [-g glob] [-F] [-i] [-m max]")
	}
	req := &proto.GrepReq{Pattern: f.pos[0], Glob: f.str("g", "glob"), Fixed: f.has("F"), Icase: f.has("i"), Max: f.num(0, "m", "max")}
	if len(f.pos) > 1 {
		req.Path = f.pos[1]
	}
	var r proto.TextReply
	if err := call(c, "POST", "/v1/grep", nil, req, &r); err != nil {
		return err
	}
	fmt.Print(r.Text)
	if r.Truncated {
		meta("结果已截断, 缩小范围或加 -m")
	}
	return nil
}

func quoteAll(a []string) []string {
	out := make([]string, len(a))
	for i, s := range a {
		if s == "" || strings.ContainsAny(s, " \t\n'\"$`\\|&;<>()*?[]{}~#!") {
			out[i] = "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
		} else {
			out[i] = s
		}
	}
	return out
}

func printExec(r *proto.ExecReply) error {
	if r.Running {
		meta("job %s 仍在运行 (%ds), 用 hxoa job %s --wait 60 继续等", r.Job, r.Millis/1000, r.Job)
	}
	os.Stdout.WriteString(r.Stdout)
	if r.Stderr != "" {
		os.Stderr.WriteString(r.Stderr)
	}
	if r.Running {
		return nil
	}
	if r.TimedOut {
		return &exitErr{124, fmt.Sprintf("超时被终止 (%dms), 用 --timeout 加长或 --bg 后台跑", r.Millis)}
	}
	if r.Code != 0 {
		return &exitErr{r.Code, fmt.Sprintf("退出码 %d", r.Code)}
	}
	return nil
}

// cmdExec: everything that is not an hxoa flag is the command. A single
// argument is passed as-is (so pipes work); several are joined with spaces.
func cmdExec(c *config, args []string) error {
	var opts, cmd []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			cmd = append(cmd, args[i+1:]...)
			break
		}
		if len(cmd) == 0 && (a == "--timeout" || a == "-t" || a == "--dir" || a == "-C") && i+1 < len(args) {
			opts = append(opts, a, args[i+1])
			i++
			continue
		}
		if a == "--skip-check" {
			opts = append(opts, a)
			continue
		}
		if len(cmd) == 0 && (a == "--bg" || a == "--stdin" || strings.HasPrefix(a, "--timeout=") || strings.HasPrefix(a, "--dir=")) {
			opts = append(opts, a)
			continue
		}
		cmd = append(cmd, args[i:]...)
		break
	}
	f := parse(opts, "timeout", "t", "dir", "C")
	if len(cmd) == 0 {
		return usage("exec [--timeout 秒] [--dir 子目录] [--bg] [--stdin] <command>")
	}
	req := &proto.ExecReq{Cmd: strings.Join(cmd, " "), Timeout: f.num(0, "timeout", "t"), Dir: f.str("dir", "C"), Bg: f.has("bg"), SkipCheck: f.has("skip-check")}
	if req.Timeout == 0 {
		req.Timeout = 120
	}
	if f.has("stdin") {
		b, _ := io.ReadAll(os.Stdin)
		req.Stdin = string(b)
	}
	// A commit may first run the workspace's checks; run it as a job and
	// wait in short polls so no single request outlives a proxy's timeout.
	wait := !req.Bg && commitRe.MatchString(req.Cmd)
	if wait {
		req.Bg = true
	}
	var r proto.ExecReply
	if err := call(c, "POST", "/v1/exec", nil, req, &r); err != nil {
		return err
	}
	if wait {
		meta("commit 前先跑 checks.yml (job %s), 等结果", r.Job)
		for r.Running {
			if err := call(c, "GET", "/v1/job", url.Values{"id": {r.Job}, "wait": {"60"}}, nil, &r); err != nil {
				return err
			}
		}
		return printExec(&r)
	}
	if req.Bg {
		fmt.Println("job " + r.Job)
		return nil
	}
	return printExec(&r)
}

// commitRe matches the server's: a git commit anywhere in the command line.
var commitRe = regexp.MustCompile(`(^|[;&|(\s'])git\s+(-[cC]\s+\S+\s+)*commit\b`)

func cmdJob(c *config, args []string) error {
	f := parse(args, "wait", "w")
	if len(f.pos) < 1 {
		return usage("job <id> [--wait 秒] [--kill]")
	}
	q := url.Values{"id": {f.pos[0]}}
	if w := f.num(0, "wait", "w"); w > 0 {
		q.Set("wait", strconv.Itoa(w))
	}
	if f.has("kill") {
		q.Set("kill", "1")
	}
	var r proto.ExecReply
	if err := call(c, "GET", "/v1/job", q, nil, &r); err != nil {
		return err
	}
	return printExec(&r)
}

// cmdPull downloads a subtree into a local directory, for building or
// analysing locally without one request per file.
func cmdPull(c *config, args []string) error {
	f := parse(args)
	if len(f.pos) < 2 {
		return usage("pull <remote-path> <local-dir> [--all 包含 node_modules/.git]")
	}
	q := url.Values{"path": {f.pos[0]}}
	if f.has("all") {
		q.Set("all", "1")
	}
	u := strings.TrimRight(c.Base, "/") + "/v1/tar?" + q.Encode()
	req, _ := http.NewRequest("GET", u, nil)
	req.Header.Set("Authorization", "Bearer "+c.Token)
	resp, err := (&http.Client{Timeout: 30 * time.Minute}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	dest := f.pos[1]
	n := 0
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		p := filepath.Join(dest, filepath.FromSlash(h.Name))
		if !strings.HasPrefix(filepath.Clean(p), filepath.Clean(dest)) {
			continue
		}
		switch h.Typeflag {
		case tar.TypeDir:
			os.MkdirAll(p, 0o755)
		case tar.TypeSymlink:
			os.MkdirAll(filepath.Dir(p), 0o755)
			os.Remove(p)
			os.Symlink(h.Linkname, p)
		case tar.TypeReg:
			os.MkdirAll(filepath.Dir(p), 0o755)
			w, err := os.OpenFile(p, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(h.Mode)&0o777|0o200)
			if err != nil {
				return err
			}
			io.Copy(w, tr)
			w.Close()
			n++
		}
	}
	fmt.Printf("ok %d 个文件 -> %s\n", n, dest)
	return nil
}
