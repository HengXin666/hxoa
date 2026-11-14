// Package main implements a small local workspace mapping client.
// Settings are kept in a per-user config file and never baked into the binary.
package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
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

func sessionConfigPath(key string) string {
	name := keyTag(key)
	if name == "" {
		return configPath()
	}
	return filepath.Join(homeDir(), configDir, name+".json")
}

func keyTag(key string) string {
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-"
	tag := make([]byte, 0, 8)
	for i := 0; i < len(key) && len(tag) < 8; i++ {
		c := key[i]
		for _, ok := range chars {
			if c == byte(ok) {
				tag = append(tag, c)
				break
			}
		}
	}
	return string(tag)
}

func latestConfigPath() string {
	dir := filepath.Join(homeDir(), configDir)
	rows, err := os.ReadDir(dir)
	if err != nil {
		return configPath()
	}
	best, bestTime := "", time.Time{}
	for _, row := range rows {
		if row.IsDir() || filepath.Ext(row.Name()) != ".json" {
			continue
		}
		path := filepath.Join(dir, row.Name())
		info, err := row.Info()
		if err != nil {
			continue
		}
		if best == "" || info.ModTime().After(bestTime) {
			best, bestTime = path, info.ModTime()
		}
	}
	if best == "" {
		return configPath()
	}
	return best
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
	raw, err := os.ReadFile(latestConfigPath())
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
	return saveConfigTo(doc, sessionConfigPath(doc.Key))
}

func saveConfigTo(doc *config, path string) error {
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
