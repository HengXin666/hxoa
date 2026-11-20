// Package main: run one command inside the mapped workspace.
package main

import (
	"encoding/json"
	"fmt"
	"os"
)

const execPath = "/fs/exec"

type execReply struct {
	OK     bool   `json:"ok"`
	Code   int    `json:"code"`
	Cwd    string `json:"cwd"`
	Stdout string `json:"stdout"`
	Stderr string `json:"stderr"`
	Error  string `json:"error"`
}

func fsExec(doc *config, args []string) int {
	if len(args) < 1 {
		return usage("exec needs a command")
	}
	cwd := "/"
	if len(doc.Roots) > 0 {
		cwd = doc.Roots[0]
	}
	payload, _ := json.Marshal(map[string]any{
		"cwd":     cwd,
		"command": joinArgs(args),
	})
	got, err := call(doc, "POST", execPath, payload, "application/json")
	if err != nil {
		return fail(err)
	}
	var out execReply
	if err := json.Unmarshal(got.body, &out); err != nil {
		_, _ = os.Stdout.Write(got.body)
		return 0
	}
	if out.Error != "" {
		fmt.Fprintln(os.Stderr, appName+": "+out.Error)
		return 1
	}
	_, _ = os.Stdout.WriteString(out.Stdout)
	if out.Stderr != "" {
		_, _ = os.Stderr.WriteString(out.Stderr)
	}
	if out.OK {
		return 0
	}
	return 1
}

// joinArgs joins command words with one space, keeping argument boundaries.
func joinArgs(args []string) string {
	out := ""
	for i, a := range args {
		if i > 0 {
			out += " "
		}
		out += a
	}
	return out
}
