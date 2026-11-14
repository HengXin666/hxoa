// Package main: workspace file commands and the local plan note.
package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"os"
	"strings"
)

const (
	fsPrefix    = "/fs"
	planDir     = ".hark"
	planFile    = "plan.md"
	contentType = "text/plain; charset=utf-8"
)

type entry struct {
	Name  string `json:"name"`
	Kind  string `json:"kind"`
	Bytes int64  `json:"bytes"`
}

func fsList(doc *config, args []string) int {
	path := "/"
	if len(args) > 0 {
		path = args[0]
	}
	got, err := call(doc, "GET", fsPrefix+path, nil, "")
	if err != nil {
		return fail(err)
	}
	var rows []entry
	if err := json.Unmarshal(got.body, &rows); err != nil {
		fmt.Println(string(got.body))
		return 0
	}
	for _, row := range rows {
		fmt.Printf("%s\t%s\t%d\n", row.Kind, row.Name, row.Bytes)
	}
	return 0
}

func fsCat(doc *config, args []string) int {
	if len(args) < 1 {
		return usage("cat needs a path")
	}
	got, err := call(doc, "GET", fsPrefix+"/"+strings.TrimPrefix(args[0], "/"), nil, "")
	if err != nil {
		return fail(err)
	}
	_, _ = os.Stdout.Write(got.body)
	return 0
}

func fsGit(doc *config, args []string) int {
	if len(args) < 1 {
		return usage("git needs a read-only subcommand")
	}
	payload, _ := json.Marshal(map[string]any{"args": args})
	got, err := call(doc, "POST", fsPrefix+"/git", payload, "application/json")
	if err != nil {
		return fail(err)
	}
	_, _ = os.Stdout.Write(got.body)
	return 0
}

func fsRoot(doc *config) int {
	if len(doc.Roots) > 0 {
		fmt.Println(doc.Roots[0])
	} else {
		fmt.Println("/")
	}
	return 0
}

// fsPlan saves a note as a local file beside the client config. It never sends
// anything over the network, so the note stays on this machine only.
func fsPlan(args []string) int {
	if len(args) < 1 {
		return usage("plan needs a local file")
	}
	raw, err := os.ReadFile(args[0])
	if err != nil {
		return fail(err)
	}
	dir := filepath.Join(homeDir(), "."+appName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fail(err)
	}
	target := filepath.Join(dir, planFile)
	if err := os.WriteFile(target, raw, 0o600); err != nil {
		return fail(err)
	}
	fmt.Println(target)
	return 0
}

func fail(err error) int {
	fmt.Fprintln(os.Stderr, appName+": "+err.Error())
	return 1
}

func usage(msg string) int {
	fmt.Fprintln(os.Stderr, appName+": "+msg)
	return 1
}
