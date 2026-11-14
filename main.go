// Package main: hxoa maps a local workspace to a remote agent over a tunnel.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		help()
		return
	}
	switch args[0] {
	case "help", "-h", "--help":
		help()
	case "login":
		os.Exit(doLogin(args[1:]))
	case "ls":
		os.Exit(withConfig(func(doc *config) int { return fsList(doc, args[1:]) }))
	case "cat":
		os.Exit(withConfig(func(doc *config) int { return fsCat(doc, args[1:]) }))
	case "root":
		os.Exit(withConfig(fsRoot))
	case "git":
		os.Exit(withConfig(func(doc *config) int { return fsGit(doc, args[1:]) }))
	case "plan", "--w_plan":
		os.Exit(fsPlan(args[1:]))
	default:
		fmt.Fprintln(os.Stderr, appName+": unknown command "+args[0])
		os.Exit(1)
	}
}

func withConfig(run func(*config) int) int {
	doc, err := readConfig()
	if err != nil {
		return fail(err)
	}
	return run(doc)
}

func doLogin(args []string) int {
	token, base := "", ""
	for i := 0; i < len(args); i++ {
		if args[i] == "--base" && i+1 < len(args) {
			i++
			base = args[i]
		} else if token == "" {
			token = args[i]
		}
	}
	if token == "" {
		return usage("login needs a token")
	}
	base = baseFromArgsAndEnv(base)
	if base == "" {
		return usage("login needs --base <url> or HARK_BASE")
	}
	probe := &config{Base: base, UA: appName + "/1"}
	got, err := callWith(probe, token, "POST", loginPath, []byte(token), "text/plain; charset=utf-8")
	if err != nil {
		return fail(err)
	}
	var doc config
	if err := json.Unmarshal(got.body, &doc); err != nil {
		return fail(err)
	}
	doc.Base = base
	if err := saveConfig(&doc); err != nil {
		return fail(err)
	}
	fmt.Println("connected to local workspace")
	return 0
}

func help() {
	lines := []string{
		appName + " ls [path]        list a directory",
		appName + " cat <path>       print a file",
		appName + " root             print the workspace root",
		appName + " git <command>    read-only git query in the workspace",
		appName + " plan <file>      save a note file to the workspace",
		appName + " --w_plan <file>  same as plan",
		appName + " login <token>    connect with --base <url> or HARK_BASE",
		"",
		"paths are relative to the workspace root",
	}
	fmt.Fprintln(os.Stdout, strings.Join(lines, "\n"))
}
