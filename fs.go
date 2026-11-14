// Package main: workspace file commands and a local note.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	fsPrefix    = "/fs"
	noteFile    = "note.md"
	contentType = "text/plain; charset=utf-8"
	replaceOp   = "replace_lines"
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
	path := fsPrefix + "/" + strings.TrimPrefix(args[0], "/")
	if len(args) > 1 {
		span := "?start=" + args[1]
		if len(args) > 2 {
			span += "&end=" + args[2]
		}
		path += span
	}
	got, err := call(doc, "GET", path, nil, "")
	if err != nil {
		return fail(err)
	}
	_, _ = os.Stdout.Write(got.body)
	return 0
}

func fsPatch(doc *config, args []string) int {
	if len(args) < 4 {
		return usage("patch needs a path, start, end and version")
	}
	start, err1 := strconv.Atoi(args[1])
	end, err2 := strconv.Atoi(args[2])
	if err1 != nil || err2 != nil || start < 1 || end < start {
		return usage("patch needs 1-based start and end with end >= start")
	}
	raw, err := io.ReadAll(os.Stdin)
	if err != nil {
		return fail(err)
	}
	text := strings.TrimRight(string(raw), "\n")
	var lines []string
	if text != "" {
		lines = strings.Split(text, "\n")
	}
	payload, _ := json.Marshal(map[string]any{
		"op": replaceOp, "start": start, "end": end,
		"lines": lines, "if_match": args[3],
	})
	path := fsPrefix + "/" + strings.TrimPrefix(args[0], "/")
	got, err := call(doc, "PATCH", path, payload, "application/json")
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

func fsPut(doc *config, args []string) int {
	if len(args) < 1 {
		return usage("put needs a path")
	}
	raw, err := io.ReadAll(os.Stdin)
	if err != nil {
		return fail(err)
	}
	path := fsPrefix + "/" + strings.TrimPrefix(args[0], "/")
	got, err := call(doc, "PUT", path, raw, contentType)
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

// fsNote saves a note as a local file beside the client config. It never sends
// anything over the network, so the note stays on this machine only.
func fsNote(args []string) int {
	if len(args) < 1 {
		return usage("note needs a local file")
	}
	raw, err := os.ReadFile(args[0])
	if err != nil {
		return fail(err)
	}
	dir := filepath.Join(homeDir(), "."+appName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fail(err)
	}
	target := filepath.Join(dir, noteFile)
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
