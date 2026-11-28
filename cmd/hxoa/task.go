package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/HengXin666/hxoa/internal/proto"
)

// cmdTask shows or sets what this connection is working on, so the user sees
// on the panel which identity holds which issue.
//
//	hxoa task                      show
//	hxoa task 104 '#108' -m 说明    set (replaces the old one)
//	hxoa task --clear              clear
func cmdTask(c *config, args []string) error {
	f := parse(args, "m", "note", "paths", "p")
	var reply struct {
		Task  *proto.Task `json:"task"`
		Brief string      `json:"brief"`
	}
	if len(f.pos) == 0 && !f.has("m", "note", "clear", "done", "paths", "p") {
		if err := call(c, "GET", "/v1/task", nil, nil, &reply); err != nil {
			return err
		}
		printTask(reply.Task)
		return nil
	}
	req := proto.TaskReq{Note: f.str("m", "note")}
	if p := f.str("paths", "p"); p != "" {
		req.Paths = []string{p}
	}
	if !f.has("clear", "done") {
		for _, a := range f.pos {
			for _, p := range strings.FieldsFunc(a, func(r rune) bool { return r == ',' || r == ' ' }) {
				n, err := strconv.Atoi(strings.TrimPrefix(p, "#"))
				if err != nil || n <= 0 {
					return usage("issue 号要是数字, 如 hxoa task 104 108")
				}
				req.Issues = append(req.Issues, n)
			}
		}
	} else {
		req.Note = ""
	}
	if err := call(c, "POST", "/v1/task", nil, req, &reply); err != nil {
		return err
	}
	printTask(reply.Task)
	if reply.Brief != "" {
		fmt.Println()
		fmt.Println(reply.Brief)
	}
	return nil
}

func cmdBrief(c *config) error {
	var r proto.BriefReply
	if err := call(c, "GET", "/v1/brief", nil, nil, &r); err != nil {
		return err
	}
	fmt.Println(r.Text)
	return nil
}

// cmdSay: hxoa say '做了什么, 影响谁' [--to oa_1]
func cmdSay(c *config, args []string) error {
	f := parse(args, "to")
	text := strings.Join(f.pos, " ")
	if strings.TrimSpace(text) == "" {
		return usage("say '做了什么, 影响谁' [--to 身份]")
	}
	var r struct {
		To []string `json:"to"`
	}
	if err := call(c, "POST", "/v1/say", nil, proto.SayReq{Text: text, To: f.str("to")}, &r); err != nil {
		return err
	}
	if len(r.To) == 0 {
		fmt.Println("现在没有其它连接, 已记进协作记录")
		return nil
	}
	fmt.Println("已告诉 " + strings.Join(r.To, ", ") + " (它下次跑 hxoa 时会看到)")
	return nil
}

// cmdLearn: hxoa learn '一句能判断对错的规则'
func cmdLearn(c *config, args []string) error {
	text := strings.Join(args, " ")
	if strings.TrimSpace(text) == "" {
		return usage("learn '一句能判断对错的规则'")
	}
	if err := call(c, "POST", "/v1/learn", nil, proto.LearnReq{Text: text}, nil); err != nil {
		return err
	}
	fmt.Println("已放进经验收件箱, 主人在面板采纳后进 brief")
	return nil
}

// printNotices shows what rode along on a response: messages from other
// workers, directory notes, overlap warnings.
func printNotices(h string) {
	if h == "" {
		return
	}
	raw, err := base64.StdEncoding.DecodeString(h)
	var list []string
	if err != nil || json.Unmarshal(raw, &list) != nil {
		return
	}
	for _, n := range list {
		for _, l := range strings.Split(n, "\n") {
			fmt.Fprintln(os.Stderr, "# "+l)
		}
	}
}

func printTask(t *proto.Task) {
	if t == nil {
		fmt.Println("当前没有登记任务")
		return
	}
	var parts []string
	for _, n := range t.Issues {
		parts = append(parts, fmt.Sprintf("#%d", n))
	}
	if t.Note != "" {
		parts = append(parts, t.Note)
	}
	who := "你自己"
	if t.By == "user" {
		who = "用户"
	}
	fmt.Printf("当前任务 (%s登记): %s\n", who, strings.Join(parts, " "))
}

// cmdMerge merges this worker's branch into the user's current branch, the
// same as the panel's merge button; used when the repo has no remote.
func cmdMerge(c *config) error {
	var reply struct {
		Base   string `json:"base"`
		Output string `json:"output"`
	}
	if err := call(c, "POST", "/v1/merge", nil, map[string]any{}, &reply); err != nil {
		return err
	}
	fmt.Printf("已合入 %s\n%s\n", reply.Base, strings.TrimSpace(reply.Output))
	return nil
}
