package server

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Check is one entry of a project's checks.yml:
//
//	checks:
//	  - name: typecheck
//	    run: npm run typecheck
//	    paths: [protocol, server]   # optional: only when a changed file is under one
//	    timeout: 600                # seconds, default 600
type Check struct {
	Name    string
	Run     string
	Paths   []string
	Timeout int
}

const (
	defaultCheckTimeout = 600
	// gateFailCode is the exit code of a commit refused by its checks.
	gateFailCode = 97
)

// CheckState is the last commit gate a connection went through, for the panel.
type CheckState struct {
	At      time.Time `json:"at"`
	OK      bool      `json:"ok"`
	Skipped bool      `json:"skipped,omitempty"`
	Failed  []string  `json:"failed,omitempty"`
}

// commitRe spots a git commit anywhere in a shell command line.
var commitRe = regexp.MustCompile(`(^|[;&|(\s'])git\s+(-[cC]\s+\S+\s+)*commit\b`)

func isCommit(cmd string) bool { return commitRe.MatchString(cmd) }

// pushRe spots a git push: what goes to the mainline is checked again after
// the agent merged the latest base into its branch.
var pushRe = regexp.MustCompile(`(^|[;&|(\s'])git\s+(-[cC]\s+\S+\s+)*push\b`)

func isPush(cmd string) bool { return pushRe.MatchString(cmd) }

// pushedFiles are the files this branch changes against the remote base
// (falling back to the local base branch).
func pushedFiles(root, base string) []string {
	for _, ref := range []string{"origin/" + base, base} {
		if s, err := git(root, "diff", "--name-only", ref+"...HEAD"); err == nil {
			var out []string
			for _, f := range strings.Split(s, "\n") {
				if f = strings.TrimSpace(f); f != "" {
					out = append(out, f)
				}
			}
			return out
		}
	}
	return nil
}

// parseChecks reads the small YAML shape above; anything else is ignored.
func parseChecks(s string) []Check {
	var out []Check
	var cur *Check
	for _, raw := range strings.Split(s, "\n") {
		l := strings.TrimSpace(raw)
		if l == "" || strings.HasPrefix(l, "#") || l == "checks:" {
			continue
		}
		if strings.HasPrefix(l, "- ") {
			out = append(out, Check{Timeout: defaultCheckTimeout})
			cur = &out[len(out)-1]
			l = strings.TrimSpace(l[2:])
		}
		if cur == nil {
			continue
		}
		k, v, ok := strings.Cut(l, ":")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		switch strings.TrimSpace(k) {
		case "name":
			cur.Name = unquote(v)
		case "run":
			cur.Run = unquote(v)
		case "timeout":
			if n, err := strconv.Atoi(unquote(v)); err == nil && n > 0 {
				cur.Timeout = n
			}
		case "paths":
			v = strings.TrimSuffix(strings.TrimPrefix(v, "["), "]")
			for _, p := range strings.Split(v, ",") {
				if p = strings.Trim(unquote(strings.TrimSpace(p)), "/"); p != "" {
					cur.Paths = append(cur.Paths, p)
				}
			}
		}
	}
	valid := out[:0]
	for _, c := range out {
		if c.Run != "" {
			if c.Name == "" {
				c.Name = c.Run
			}
			valid = append(valid, c)
		}
	}
	return valid
}

// changedFiles is what the commit is likely to take: staged, unstaged and
// untracked files (a "git add -A && git commit" has not staged yet).
func changedFiles(root string) []string {
	seen := map[string]bool{}
	var out []string
	for _, args := range [][]string{
		{"diff", "--cached", "--name-only"},
		{"diff", "--name-only", "HEAD"},
		{"ls-files", "--others", "--exclude-standard"},
	} {
		s, err := git(root, args...)
		if err != nil {
			continue
		}
		for _, f := range strings.Split(s, "\n") {
			if f = strings.TrimSpace(f); f != "" && !seen[f] {
				seen[f] = true
				out = append(out, f)
			}
		}
	}
	return out
}

// applicable keeps the checks whose paths cover a changed file.
func applicable(checks []Check, files []string) []Check {
	var out []Check
	for _, c := range checks {
		if len(c.Paths) == 0 {
			out = append(out, c)
			continue
		}
	hit:
		for _, p := range c.Paths {
			for _, f := range files {
				if f == p || strings.HasPrefix(f, p+"/") {
					out = append(out, c)
					break hit
				}
			}
		}
	}
	return out
}

func shQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// gateScript runs the checks in order, then the original command only when
// all passed; refused commits exit with gateFailCode. Returns the script and
// the extra seconds it may take.
func gateScript(checks []Check, cmd string) (string, int) {
	var b strings.Builder
	extra := 0
	b.WriteString("__hxoa_failed=''\n")
	for _, c := range checks {
		extra += c.Timeout
		fmt.Fprintf(&b, "echo %s\n", shQuote("== hxoa check: "+c.Name))
		fmt.Fprintf(&b, "timeout %d bash -lc %s || __hxoa_failed=\"$__hxoa_failed %s\"\n", c.Timeout, shQuote(c.Run), strings.NewReplacer(`"`, "", " ", "_", "$", "", "`", "").Replace(c.Name))
	}
	fmt.Fprintf(&b, "if [ -n \"$__hxoa_failed\" ]; then echo \"hxoa: checks 没过:$__hxoa_failed, 已拒绝 (commit / push 没执行)。修好再来; 确实要跳过: 命令前加 --skip-check, 如 hxoa git --skip-check push ... (面板会标红)\" >&2; exit %d; fi\n", gateFailCode)
	b.WriteString(cmd + "\n")
	return b.String(), extra
}

// failedOf reads the failed check names back from the job's stderr.
func failedOf(stderr string) []string {
	const mark = "hxoa: checks 没过:"
	i := strings.LastIndex(stderr, mark)
	if i < 0 {
		return nil
	}
	rest := stderr[i+len(mark):]
	if j := strings.Index(rest, ","); j >= 0 {
		rest = rest[:j]
	}
	return strings.Fields(rest)
}
