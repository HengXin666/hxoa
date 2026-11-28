package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Panel side of the collaboration memory: which project dir a workspace
// uses, brief length, the lesson inbox (adopt or drop) and recent messages.
func (s *Server) memoryRoutes(mux *http.ServeMux) {
	ws := func(w http.ResponseWriter, r *http.Request) *Workspace {
		if s.Memory == nil {
			writeJSON(w, 503, map[string]any{"ok": false, "error": "协作记忆没打开, 看 hxoad 日志"})
			return nil
		}
		v := s.Store.Workspace(r.PathValue("id"))
		if v == nil {
			writeJSON(w, 404, map[string]any{"ok": false, "error": "工作区不存在"})
		}
		return v
	}
	mux.HandleFunc("GET /api/workspaces/{id}/memory", func(w http.ResponseWriter, r *http.Request) {
		v := ws(w, r)
		if v == nil {
			return
		}
		p := s.Memory.Route(v.ID)
		out := map[string]any{"ok": true, "dir": p, "projects": s.Memory.Projects(), "root": s.Memory.Dir, "remote": s.Memory.Remote}
		if p != "" {
			brief := s.Memory.read(p, briefFile)
			lines := 0
			if brief != "" {
				lines = strings.Count(brief, "\n") + 1
			}
			var checks []string
			for _, c := range parseChecks(s.Memory.read(p, checksFile)) {
				checks = append(checks, c.Name)
			}
			inbox := s.Memory.Inbox(p)
			if inbox == nil {
				inbox = []string{}
			}
			threads := s.Memory.Threads(p, threadTail)
			if threads == nil {
				threads = []string{}
			}
			out["brief_lines"] = lines
			out["brief_max"] = BriefMaxLine
			out["checks"] = checks
			out["inbox"] = inbox
			out["threads"] = threads
		}
		writeJSON(w, 200, out)
	})
	mux.HandleFunc("PUT /api/workspaces/{id}/memory", func(w http.ResponseWriter, r *http.Request) {
		v := ws(w, r)
		if v == nil {
			return
		}
		var in struct {
			Dir string `json:"dir"`
		}
		json.NewDecoder(r.Body).Decode(&in)
		if err := s.Memory.SetRoute(v.ID, in.Dir); err != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		s.ops.add(opEntry{At: time.Now(), Who: actor(r), WS: v.Name, Op: "memory", Target: in.Dir, OK: true})
		writeJSON(w, 200, map[string]any{"ok": true})
	})
	mux.HandleFunc("POST /api/workspaces/{id}/inbox/{i}/{act}", func(w http.ResponseWriter, r *http.Request) {
		v := ws(w, r)
		if v == nil {
			return
		}
		p := s.Memory.Route(v.ID)
		i, err := strconv.Atoi(r.PathValue("i"))
		act := r.PathValue("act")
		if p == "" || err != nil || (act != "adopt" && act != "drop") {
			writeJSON(w, 400, map[string]any{"ok": false, "error": "用法: POST .../inbox/<序号>/adopt|drop"})
			return
		}
		var in struct {
			To string `json:"to"`
		}
		json.NewDecoder(r.Body).Decode(&in)
		if err := s.Memory.Resolve(p, i, act == "adopt", in.To); err != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		s.ops.add(opEntry{At: time.Now(), Who: actor(r), WS: v.Name, Op: "inbox " + act, Target: strconv.Itoa(i + 1), OK: true})
		writeJSON(w, 200, map[string]any{"ok": true})
	})
}
