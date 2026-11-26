package main

import (
	"fmt"
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
	f := parse(args, "m", "note")
	var reply struct {
		Task *proto.Task `json:"task"`
	}
	if len(f.pos) == 0 && !f.has("m", "note", "clear", "done") {
		if err := call(c, "GET", "/v1/task", nil, nil, &reply); err != nil {
			return err
		}
		printTask(reply.Task)
		return nil
	}
	req := proto.TaskReq{Note: f.str("m", "note")}
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
	return nil
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
