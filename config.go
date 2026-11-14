// Package main implements a small local workspace mapping client.
// Settings are kept in a per-user config file and never baked into the binary.
package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

const (
	appName    = "hxoa"
	configDir  = "." + appName
	configFile = "config.json"
	loginPath  = "/.hxsz/login"
	envBase    = "HARK_BASE"
)

type config struct {
	Base  string   `json:"base"`
	Token string   `json:"token"`
	Key   string   `json:"key"`
	UA    string   `json:"ua"`
	Roots []string `json:"roots"`
	Ver   int      `json:"ver"`
}

func configPath() string {
	return filepath.Join(homeDir(), configDir, configFile)
}

func homeDir() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	dir, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return dir
}

func readConfig() (*config, error) {
	raw, err := os.ReadFile(configPath())
	if err != nil {
		return nil, errors.New("no connection yet, run: hxoa login <token> --base <url>")
	}
	var doc config
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	overrideBase(&doc)
	return &doc, nil
}

func overrideBase(doc *config) {
	if base := os.Getenv(envBase); base != "" {
		doc.Base = base
	}
}

func saveConfig(doc *config) error {
	path := configPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o600)
}

func baseFromArgsAndEnv(flagBase string) string {
	if flagBase != "" {
		return flagBase
	}
	return os.Getenv(envBase)
}
