// Package proto holds the wire types shared by the hxoa client and the hxoad server.
//
// Every agent request carries "Authorization: Bearer <token>". Every response is
// JSON; failures are {"ok":false,"error":"...","code":"..."} with an HTTP status
// that matches the code (409 for conflicts).
package proto

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

const (
	// Version is bumped when the wire protocol changes incompatibly.
	Version = 2

	CodeConflict   = "conflict"     // file changed since it was read
	CodeNotFound   = "not_found"    // path does not exist
	CodeExists     = "exists"       // create-only write hit an existing file
	CodeBadRequest = "bad_request"  // malformed input
	CodeDenied     = "denied"       // path escapes the workspace, or feature off
	CodeAuth       = "unauthorized" // token unknown or revoked
	CodeTooLarge   = "too_large"    // read without a range on a big file
	CodeAmbiguous  = "ambiguous"    // replace: old text matched more than once
	CodeNoMatch    = "no_match"     // replace: old text not found
)

// Hash returns the short content version used for optimistic concurrency.
func Hash(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:8])
}

// RangeHash versions a span of lines, so a ranged read can be patched back
// without re-reading the whole file.
func RangeHash(lines []string) string {
	return Hash([]byte(strings.Join(lines, "\n")))
}

type Error struct {
	OK    bool   `json:"ok"`
	Error string `json:"error"`
	Code  string `json:"code,omitempty"`
	// Current is filled on conflicts so the caller knows what to re-read.
	Current string `json:"current_version,omitempty"`
}

type Hello struct {
	OK        bool   `json:"ok"`
	Server    string `json:"server"`
	Proto     int    `json:"proto"`
	Workspace string `json:"workspace"`
	Identity  string `json:"identity"`
	Root      string `json:"root"`
	Mode      string `json:"mode"` // "direct" or "worker"
	Branch    string `json:"branch,omitempty"`
	Base      string `json:"base_branch,omitempty"`
	Exec      bool   `json:"exec"`
}

type Entry struct {
	Path  string `json:"p"`
	Dir   bool   `json:"d,omitempty"`
	Bytes int64  `json:"b,omitempty"`
}

type LsReply struct {
	OK        bool    `json:"ok"`
	Entries   []Entry `json:"entries"`
	Truncated bool    `json:"truncated,omitempty"`
}

type ReadReply struct {
	OK      bool   `json:"ok"`
	Path    string `json:"path"`
	Version string `json:"version"`
	Lines   int    `json:"lines"` // total lines in file
	Start   int    `json:"start,omitempty"`
	End     int    `json:"end,omitempty"`
	// RangeVersion versions only Start..End, usable as range_match in a patch.
	RangeVersion string `json:"range_version,omitempty"`
	Content      string `json:"content"`
	Binary       bool   `json:"binary,omitempty"`
}

// WriteReq writes a whole file. IfMatch: the version read earlier; "new" to
// require that the file does not exist; "*" to overwrite blindly.
type WriteReq struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	IfMatch string `json:"if_match"`
}

// Edit is one change inside a patch.
//   - op "lines": replace lines Start..End (1-based, inclusive) with Lines.
//     Insert before line N with Start=N, End=N-1. Numbers refer to the file as
//     it was read, even when several line edits are sent together.
//   - op "replace": replace Old with New; Old must occur exactly once unless All.
type Edit struct {
	Op    string   `json:"op"`
	Start int      `json:"start,omitempty"`
	End   int      `json:"end,omitempty"`
	Lines []string `json:"lines,omitempty"`
	Old   string   `json:"old,omitempty"`
	New   string   `json:"new,omitempty"`
	All   bool     `json:"all,omitempty"`
}

// PatchReq applies edits atomically. Exactly one guard is required: IfMatch
// (whole-file version) or RangeMatch (range_version of a ranged read, valid
// only with a single "lines" edit over that same range).
type PatchReq struct {
	Path       string `json:"path"`
	IfMatch    string `json:"if_match,omitempty"`
	RangeMatch string `json:"range_match,omitempty"`
	Edits      []Edit `json:"edits"`
}

type WriteReply struct {
	OK      bool   `json:"ok"`
	Path    string `json:"path"`
	Version string `json:"version"`
	Lines   int    `json:"lines"`
	Created bool   `json:"created,omitempty"`
}

type RmReq struct {
	Path    string `json:"path"`
	IfMatch string `json:"if_match"`
}

// MkdirReq creates a directory (and missing parents). An existing directory is fine.
type MkdirReq struct {
	Path string `json:"path"`
}

type MvReq struct {
	From    string `json:"from"`
	To      string `json:"to"`
	IfMatch string `json:"if_match"`
}

type GrepReq struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path,omitempty"`
	Glob    string `json:"glob,omitempty"`
	Fixed   bool   `json:"fixed,omitempty"`
	Icase   bool   `json:"icase,omitempty"`
	Max     int    `json:"max,omitempty"`
}

type TextReply struct {
	OK        bool   `json:"ok"`
	Text      string `json:"text"`
	Truncated bool   `json:"truncated,omitempty"`
}

type ExecReq struct {
	Cmd     string `json:"cmd"`
	Dir     string `json:"dir,omitempty"` // relative to the root
	Timeout int    `json:"timeout,omitempty"`
	Bg      bool   `json:"bg,omitempty"`
	Stdin   string `json:"stdin,omitempty"`
}

type ExecReply struct {
	OK        bool   `json:"ok"`
	Job       string `json:"job,omitempty"`
	Running   bool   `json:"running,omitempty"`
	Code      int    `json:"code"`
	Stdout    string `json:"stdout"`
	Stderr    string `json:"stderr"`
	Truncated bool   `json:"truncated,omitempty"`
	TimedOut  bool   `json:"timed_out,omitempty"`
	Millis    int64  `json:"ms"`
}

// Target lets the client forget a cached version when a write conflicts.
func (r *WriteReq) Target() string { return r.Path }
func (r *PatchReq) Target() string { return r.Path }
func (r *RmReq) Target() string    { return r.Path }
func (r *MvReq) Target() string    { return r.From }
