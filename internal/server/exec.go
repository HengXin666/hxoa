package server

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/HengXin666/hxoa/internal/proto"
)

const (
	outCap         = 64 << 10 // per stream returned to the agent
	defaultTimeout = 120
	maxTimeout     = 1800
)

// capBuf keeps the head and tail of a long stream: the error that ends a
// build is usually at the bottom, the command echo at the top.
type capBuf struct {
	mu        sync.Mutex
	head      bytes.Buffer
	tail      []byte
	truncated bool
}

func (c *capBuf) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := len(p)
	if room := outCap/2 - c.head.Len(); room > 0 {
		k := min(room, len(p))
		c.head.Write(p[:k])
		p = p[k:]
	}
	if len(p) > 0 {
		c.tail = append(c.tail, p...)
		if len(c.tail) > outCap/2 {
			c.truncated = true
			c.tail = c.tail[len(c.tail)-outCap/2:]
		}
	}
	return n, nil
}

func (c *capBuf) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.truncated {
		return c.head.String() + "\n…[输出过长, 中间已省略]…\n" + string(c.tail)
	}
	return c.head.String() + string(c.tail)
}

type job struct {
	id       string
	owner    string
	cmd      *exec.Cmd
	stdout   *capBuf
	stderr   *capBuf
	started  time.Time
	done     chan struct{}
	code     int
	timedOut bool
	ended    time.Time
}

var jobs sync.Map // id -> *job

func shell() string {
	if p, err := exec.LookPath("bash"); err == nil {
		return p
	}
	return "/bin/sh"
}

func startJob(root, owner string, req *proto.ExecReq) (*job, error) {
	dir := root
	if req.Dir != "" {
		d, err := resolve(root, req.Dir, false)
		if err != nil {
			return nil, err
		}
		dir = d
	}
	t := req.Timeout
	if t <= 0 {
		t = defaultTimeout
	}
	if t > maxTimeout {
		t = maxTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(t)*time.Second)
	cmd := exec.CommandContext(ctx, shell(), "-lc", req.Cmd)
	cmd.Dir = dir
	cmd.Env = append(scrubEnv(os.Environ()), "HXOA=1", "GIT_TERMINAL_PROMPT=0", "PAGER=cat", "GIT_PAGER=cat", "TERM=dumb")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 3 * time.Second
	if req.Stdin != "" {
		cmd.Stdin = strings.NewReader(req.Stdin)
	}
	j := &job{id: randHex(4), owner: owner, cmd: cmd, stdout: &capBuf{}, stderr: &capBuf{}, started: time.Now(), done: make(chan struct{})}
	cmd.Stdout, cmd.Stderr = j.stdout, j.stderr
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, errf(proto.CodeBadRequest, err.Error())
	}
	jobs.Store(j.id, j)
	go func() {
		err := cmd.Wait()
		j.timedOut = ctx.Err() == context.DeadlineExceeded
		cancel()
		j.code = 0
		if err != nil {
			j.code = -1
			if ee, ok := err.(*exec.ExitError); ok {
				j.code = ee.ExitCode()
			}
		}
		j.ended = time.Now()
		close(j.done)
		// forget finished jobs after a while
		time.AfterFunc(30*time.Minute, func() { jobs.Delete(j.id) })
	}()
	return j, nil
}

func (j *job) reply() *proto.ExecReply {
	r := &proto.ExecReply{OK: true, Job: j.id}
	select {
	case <-j.done:
		r.Code = j.code
		r.OK = j.code == 0
		r.TimedOut = j.timedOut
		r.Millis = j.ended.Sub(j.started).Milliseconds()
	default:
		r.Running = true
		r.Millis = time.Since(j.started).Milliseconds()
	}
	r.Stdout = j.stdout.String()
	r.Stderr = j.stderr.String()
	r.Truncated = j.stdout.truncated || j.stderr.truncated
	return r
}

func doExec(root, owner string, req *proto.ExecReq) (*proto.ExecReply, error) {
	if strings.TrimSpace(req.Cmd) == "" {
		return nil, errf(proto.CodeBadRequest, "命令为空")
	}
	j, err := startJob(root, owner, req)
	if err != nil {
		return nil, err
	}
	if req.Bg {
		return &proto.ExecReply{OK: true, Job: j.id, Running: true}, nil
	}
	<-j.done
	r := j.reply()
	r.Job = ""
	jobs.Delete(j.id)
	return r, nil
}

// killJobs stops every job a deleted token left running.
func killJobs(owner string) {
	jobs.Range(func(k, v any) bool {
		j := v.(*job)
		if j.owner == owner && j.cmd.Process != nil {
			select {
			case <-j.done:
			default:
				syscall.Kill(-j.cmd.Process.Pid, syscall.SIGKILL)
			}
		}
		return true
	})
}

func jobStatus(owner, id string, wait int, kill bool) (*proto.ExecReply, error) {
	v, ok := jobs.Load(id)
	if !ok || v.(*job).owner != owner {
		return nil, errf(proto.CodeNotFound, "没有这个 job: "+id)
	}
	j := v.(*job)
	if kill {
		if j.cmd.Process != nil {
			syscall.Kill(-j.cmd.Process.Pid, syscall.SIGKILL)
		}
		<-j.done
	} else if wait > 0 {
		select {
		case <-j.done:
		case <-time.After(time.Duration(min(wait, 110)) * time.Second):
		}
	}
	return j.reply(), nil
}

func doGrep(root string, req *proto.GrepReq) (*proto.TextReply, error) {
	if req.Pattern == "" {
		return nil, errf(proto.CodeBadRequest, "pattern 为空")
	}
	dir := "."
	if req.Path != "" {
		abs, err := resolve(root, req.Path, false)
		if err != nil {
			return nil, err
		}
		dir = relTo(root, abs) // relative, so every result line stays short
	}
	max := req.Max
	if max <= 0 {
		max = 200
	}
	var args []string
	bin := "rg"
	if _, err := exec.LookPath("rg"); err == nil {
		args = []string{"-n", "--no-heading", "--color=never", "-M", "300", "--max-columns-preview", "-g", "!.hxoa"}
		if req.Fixed {
			args = append(args, "-F")
		}
		if req.Icase {
			args = append(args, "-i")
		}
		if req.Glob != "" {
			args = append(args, "-g", req.Glob)
		}
		args = append(args, "-e", req.Pattern, dir)
	} else {
		bin = "grep"
		args = []string{"-rnI", "--exclude-dir=.git", "--exclude-dir=node_modules", "--exclude-dir=.hxoa"}
		if req.Fixed {
			args = append(args, "-F")
		} else {
			args = append(args, "-E")
		}
		if req.Icase {
			args = append(args, "-i")
		}
		if req.Glob != "" {
			args = append(args, "--include="+req.Glob)
		}
		args = append(args, "-e", req.Pattern, dir)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = root
	out, _ := cmd.Output()
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimPrefix(l, "./")
	}
	r := &proto.TextReply{OK: true}
	if len(lines) == 1 && lines[0] == "" {
		return r, nil
	}
	if len(lines) > max {
		lines = lines[:max]
		r.Truncated = true
	}
	r.Text = strings.Join(lines, "\n") + "\n"
	return r, nil
}

// writeTar streams a gzip tarball of a subtree, skipping heavy dirs.
func writeTar(root, rel string, w io.Writer, skipHeavy bool) error {
	abs, err := resolve(root, rel, false)
	if err != nil {
		return err
	}
	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)
	base := filepath.Dir(abs)
	if abs == root {
		base = abs
	}
	err = filepath.WalkDir(abs, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && p != abs && (d.Name() == ".hxoa" || (skipHeavy && skipDirs[d.Name()])) {
			return filepath.SkipDir
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		link := ""
		if info.Mode()&fs.ModeSymlink != 0 {
			link, _ = os.Readlink(p)
		}
		hdr, err := tar.FileInfoHeader(info, link)
		if err != nil {
			return nil
		}
		name, _ := filepath.Rel(base, p)
		if name == "." {
			return nil
		}
		hdr.Name = filepath.ToSlash(name)
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			f, err := os.Open(p)
			if err != nil {
				return nil
			}
			io.Copy(tw, f)
			f.Close()
		}
		return nil
	})
	tw.Close()
	gz.Close()
	return err
}
