package server

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/HengXin666/hxoa/internal/proto"
)

// apiErr carries a protocol error code up to the HTTP layer.
type apiErr struct {
	code    string
	msg     string
	current string
}

func (e *apiErr) Error() string { return e.msg }

func errf(code, msg string) error { return &apiErr{code: code, msg: msg} }

// fileLocks serialise check-then-write per absolute path, so two API callers
// can never both pass the same version check.
var fileLocks sync.Map

func lockPath(p string) func() {
	v, _ := fileLocks.LoadOrStore(p, &sync.Mutex{})
	m := v.(*sync.Mutex)
	m.Lock()
	return m.Unlock
}

// resolve maps a workspace-relative path to an absolute one and refuses
// anything that escapes root, including through symlinks.
func resolve(root, rel string, write bool) (string, error) {
	rel = strings.TrimPrefix(filepath.ToSlash(rel), "/")
	for _, seg := range strings.Split(rel, "/") {
		if seg == ".." {
			return "", errf(proto.CodeDenied, "路径里不能有 ..: "+rel)
		}
	}
	clean := filepath.Clean("/" + rel)
	abs := filepath.Join(root, clean)
	// Follow symlinks on the longest existing prefix.
	probe := abs
	var tail []string
	for {
		if _, err := os.Lstat(probe); err == nil {
			break
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			break
		}
		tail = append([]string{filepath.Base(probe)}, tail...)
		probe = parent
	}
	real, err := filepath.EvalSymlinks(probe)
	if err != nil {
		return "", errf(proto.CodeDenied, "无法解析路径: "+rel)
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", errf(proto.CodeNotFound, "工作区目录不存在: "+root)
	}
	full := filepath.Join(append([]string{real}, tail...)...)
	if full != realRoot && !strings.HasPrefix(full, realRoot+string(os.PathSeparator)) {
		return "", errf(proto.CodeDenied, "路径越出工作区: "+rel)
	}
	if write {
		r, _ := filepath.Rel(realRoot, full)
		first := strings.Split(filepath.ToSlash(r), "/")[0]
		if first == ".git" || first == ".hxoa" {
			return "", errf(proto.CodeDenied, "不允许直接写 "+first+"/, 请用 exec 调 git")
		}
	}
	return full, nil
}

func relTo(root, abs string) string {
	realRoot, _ := filepath.EvalSymlinks(root)
	r, err := filepath.Rel(realRoot, abs)
	if err != nil {
		return abs
	}
	return filepath.ToSlash(r)
}

var skipDirs = map[string]bool{".git": true, ".hxoa": true, "node_modules": true}

func doLs(root, rel string, depth, max int) (*proto.LsReply, error) {
	abs, err := resolve(root, rel, false)
	if err != nil {
		return nil, err
	}
	st, err := os.Stat(abs)
	if err != nil {
		return nil, errf(proto.CodeNotFound, "不存在: "+rel)
	}
	if !st.IsDir() {
		return &proto.LsReply{OK: true, Entries: []proto.Entry{{Path: relTo(root, abs), Bytes: st.Size()}}}, nil
	}
	if depth < 1 {
		depth = 1
	}
	if max < 1 {
		max = 2000
	}
	out := &proto.LsReply{OK: true}
	base := strings.Count(abs, string(os.PathSeparator))
	_ = filepath.WalkDir(abs, func(p string, d fs.DirEntry, err error) error {
		if err != nil || p == abs {
			return nil
		}
		lvl := strings.Count(p, string(os.PathSeparator)) - base
		if d.IsDir() && skipDirs[d.Name()] {
			out.Entries = append(out.Entries, proto.Entry{Path: relTo(root, p), Dir: true})
			return filepath.SkipDir
		}
		if !d.IsDir() && d.Name() == ".git" { // a worktree's .git pointer file
			return nil
		}
		if len(out.Entries) >= max {
			out.Truncated = true
			return filepath.SkipAll
		}
		e := proto.Entry{Path: relTo(root, p), Dir: d.IsDir()}
		if !d.IsDir() {
			if info, err := d.Info(); err == nil {
				e.Bytes = info.Size()
			}
		}
		out.Entries = append(out.Entries, e)
		if d.IsDir() && lvl >= depth {
			return filepath.SkipDir
		}
		return nil
	})
	sort.Slice(out.Entries, func(i, j int) bool { return out.Entries[i].Path < out.Entries[j].Path })
	return out, nil
}

// splitLines splits text into lines without the trailing newline, and reports
// whether the file ended with one so writes can preserve it.
func splitLines(b []byte) ([]string, bool) {
	s := string(b)
	if s == "" {
		return nil, false
	}
	nl := strings.HasSuffix(s, "\n")
	s = strings.TrimSuffix(s, "\n")
	return strings.Split(s, "\n"), nl
}

func joinLines(lines []string, nl bool) []byte {
	if len(lines) == 0 {
		return nil
	}
	s := strings.Join(lines, "\n")
	if nl {
		s += "\n"
	}
	return []byte(s)
}

const maxWholeRead = 512 << 10

func doRead(root, rel string, start, end int) (*proto.ReadReply, error) {
	abs, err := resolve(root, rel, false)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(abs)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, errf(proto.CodeNotFound, "文件不存在: "+rel)
		}
		return nil, errf(proto.CodeBadRequest, err.Error())
	}
	r := &proto.ReadReply{OK: true, Path: relTo(root, abs), Version: proto.Hash(b)}
	if bytes.IndexByte(b[:min(len(b), 8000)], 0) >= 0 || !utf8.Valid(b) {
		r.Binary = true
		r.Lines = 0
		return r, nil
	}
	lines, _ := splitLines(b)
	r.Lines = len(lines)
	if start <= 0 && end <= 0 {
		if len(b) > maxWholeRead {
			return nil, errf(proto.CodeTooLarge, "文件太大, 请按行读取 (共 "+itoa(len(lines))+" 行)")
		}
		r.Content = string(b)
		return r, nil
	}
	if start < 1 {
		start = 1
	}
	if end <= 0 || end > len(lines) {
		end = len(lines)
	}
	if start > len(lines) {
		r.Start, r.End = start, start-1
		r.RangeVersion = proto.RangeHash(nil)
		return r, nil
	}
	span := lines[start-1 : end]
	r.Start, r.End = start, end
	r.RangeVersion = proto.RangeHash(span)
	r.Content = strings.Join(span, "\n") + "\n"
	return r, nil
}

func conflict(cur string) error {
	return &apiErr{code: proto.CodeConflict, msg: "文件在你读取之后被改过, 请重新读取再改", current: cur}
}

// atomicWrite writes via a temp file + rename and keeps the old mode. expect
// is the version the caller checked ("" = file must not exist): it is checked
// again right before the rename, which shrinks the window in which an editor
// outside hxoad could slip a change in to a few microseconds.
func atomicWrite(abs string, data []byte, expect string) error {
	mode := fs.FileMode(0o644)
	if st, err := os.Stat(abs); err == nil {
		mode = st.Mode().Perm()
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(abs), ".hxoa-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	tmp.Close()
	os.Chmod(tmp.Name(), mode)
	if expect != "*" {
		cur, _, exists := currentVersion(abs)
		if (expect == "" && exists) || (expect != "" && cur != expect) {
			os.Remove(tmp.Name())
			return conflict(cur)
		}
	}
	return os.Rename(tmp.Name(), abs)
}

func currentVersion(abs string) (string, []byte, bool) {
	b, err := os.ReadFile(abs)
	if err != nil {
		return "", nil, false
	}
	return proto.Hash(b), b, true
}

func doWrite(root string, req *proto.WriteReq) (*proto.WriteReply, error) {
	abs, err := resolve(root, req.Path, true)
	if err != nil {
		return nil, err
	}
	defer lockPath(abs)()
	cur, _, exists := currentVersion(abs)
	switch {
	case req.IfMatch == "*":
	case req.IfMatch == "new" || req.IfMatch == "":
		if exists {
			return nil, &apiErr{code: proto.CodeExists, msg: "文件已存在; 改已有文件需要带上读取时的 version", current: cur}
		}
	default:
		if !exists {
			return nil, errf(proto.CodeNotFound, "文件已不存在: "+req.Path)
		}
		if cur != req.IfMatch {
			return nil, conflict(cur)
		}
	}
	data := []byte(req.Content)
	expect := cur
	if req.IfMatch == "*" {
		expect = "*"
	}
	if err := atomicWrite(abs, data, expect); err != nil {
		return nil, asAPI(err)
	}
	lines, _ := splitLines(data)
	return &proto.WriteReply{OK: true, Path: relTo(root, abs), Version: proto.Hash(data), Lines: len(lines), Created: !exists}, nil
}

func doPatch(root string, req *proto.PatchReq) (*proto.WriteReply, error) {
	if len(req.Edits) == 0 {
		return nil, errf(proto.CodeBadRequest, "没有 edits")
	}
	if (req.IfMatch == "") == (req.RangeMatch == "") {
		return nil, errf(proto.CodeBadRequest, "需要且只能给一个: if_match 或 range_match")
	}
	abs, err := resolve(root, req.Path, true)
	if err != nil {
		return nil, err
	}
	defer lockPath(abs)()
	cur, b, exists := currentVersion(abs)
	if !exists {
		return nil, errf(proto.CodeNotFound, "文件不存在: "+req.Path)
	}
	lines, nl := splitLines(b)
	if req.IfMatch != "" && req.IfMatch != cur {
		return nil, conflict(cur)
	}
	if req.RangeMatch != "" {
		e := req.Edits
		if len(e) != 1 || e[0].Op != "lines" {
			return nil, errf(proto.CodeBadRequest, "range_match 只能配一个 lines 编辑")
		}
		if e[0].Start < 1 || e[0].End > len(lines) || e[0].End < e[0].Start-1 {
			return nil, conflict(cur)
		}
		if proto.RangeHash(lines[e[0].Start-1:e[0].End]) != req.RangeMatch {
			return nil, conflict(cur)
		}
	}
	var lineEdits, strEdits []proto.Edit
	for _, e := range req.Edits {
		switch e.Op {
		case "lines":
			lineEdits = append(lineEdits, e)
		case "replace":
			strEdits = append(strEdits, e)
		default:
			return nil, errf(proto.CodeBadRequest, "未知 op: "+e.Op)
		}
	}
	if len(lineEdits) > 0 && len(strEdits) > 0 {
		return nil, errf(proto.CodeBadRequest, "一次 patch 里不要混用 lines 和 replace")
	}
	var out []byte
	if len(lineEdits) > 0 {
		sort.Slice(lineEdits, func(i, j int) bool { return lineEdits[i].Start > lineEdits[j].Start })
		prevStart := len(lines) + 1
		for _, e := range lineEdits {
			if e.Start < 1 || e.Start > len(lines)+1 || e.End < e.Start-1 || e.End > len(lines) {
				return nil, errf(proto.CodeBadRequest, "行号越界 "+itoa(e.Start)+"-"+itoa(e.End)+" (共 "+itoa(len(lines))+" 行)")
			}
			if e.End >= prevStart {
				return nil, errf(proto.CodeBadRequest, "lines 编辑范围重叠")
			}
			prevStart = e.Start
			nlines := append([]string{}, lines[:e.Start-1]...)
			nlines = append(nlines, e.Lines...)
			lines = append(nlines, lines[e.End:]...)
		}
		if !nl && len(b) == 0 {
			nl = true
		}
		out = joinLines(lines, nl)
	} else {
		s := string(b)
		for i, e := range strEdits {
			if e.Old == "" {
				return nil, errf(proto.CodeBadRequest, "replace 的 old 不能为空")
			}
			n := strings.Count(s, e.Old)
			if n == 0 {
				return nil, &apiErr{code: proto.CodeNoMatch, msg: "第 " + itoa(i+1) + " 个 replace 没找到 old", current: cur}
			}
			if n > 1 && !e.All {
				return nil, &apiErr{code: proto.CodeAmbiguous, msg: "第 " + itoa(i+1) + " 个 replace 的 old 出现 " + itoa(n) + " 次, 请多带几行上下文", current: cur}
			}
			if e.All {
				s = strings.ReplaceAll(s, e.Old, e.New)
			} else {
				s = strings.Replace(s, e.Old, e.New, 1)
			}
		}
		out = []byte(s)
	}
	if err := atomicWrite(abs, out, cur); err != nil {
		return nil, asAPI(err)
	}
	nlines, _ := splitLines(out)
	return &proto.WriteReply{OK: true, Path: relTo(root, abs), Version: proto.Hash(out), Lines: len(nlines)}, nil
}

func doRm(root string, req *proto.RmReq) error {
	abs, err := resolve(root, req.Path, true)
	if err != nil {
		return err
	}
	defer lockPath(abs)()
	st, err := os.Stat(abs)
	if err != nil {
		return errf(proto.CodeNotFound, "不存在: "+req.Path)
	}
	if st.IsDir() {
		ents, _ := os.ReadDir(abs)
		if len(ents) > 0 {
			return errf(proto.CodeBadRequest, "目录不为空; 删目录请用 exec")
		}
		return os.Remove(abs)
	}
	cur, _, _ := currentVersion(abs)
	if req.IfMatch != "*" && req.IfMatch != cur {
		return conflict(cur)
	}
	return os.Remove(abs)
}

// doMkdir creates a directory inside the workspace; created is false when it
// already existed. A file in the way is an exists error.
func doMkdir(root string, req *proto.MkdirReq) (created bool, err error) {
	if strings.Trim(filepath.ToSlash(req.Path), "/") == "" {
		return false, errf(proto.CodeBadRequest, "path 必填")
	}
	abs, err := resolve(root, req.Path, true)
	if err != nil {
		return false, err
	}
	if st, e := os.Stat(abs); e == nil {
		if st.IsDir() {
			return false, nil
		}
		return false, errf(proto.CodeExists, "已有同名文件: "+req.Path)
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return false, errf(proto.CodeBadRequest, err.Error())
	}
	return true, nil
}

func doMv(root string, req *proto.MvReq) error {
	from, err := resolve(root, req.From, true)
	if err != nil {
		return err
	}
	to, err := resolve(root, req.To, true)
	if err != nil {
		return err
	}
	defer lockPath(from)()
	st, err := os.Stat(from)
	if err != nil {
		return errf(proto.CodeNotFound, "不存在: "+req.From)
	}
	if !st.IsDir() && req.IfMatch != "*" {
		cur, _, _ := currentVersion(from)
		if cur != req.IfMatch {
			return conflict(cur)
		}
	}
	if _, err := os.Stat(to); err == nil {
		return errf(proto.CodeExists, "目标已存在: "+req.To)
	}
	os.MkdirAll(filepath.Dir(to), 0o755)
	return os.Rename(from, to)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}

func asAPI(err error) error {
	if _, ok := err.(*apiErr); ok {
		return err
	}
	return errf(proto.CodeBadRequest, err.Error())
}
