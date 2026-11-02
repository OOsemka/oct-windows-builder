package main

import (
	"encoding/json"
	"net/http"
	"strings"
)

func (m *Manager) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func (m *Manager) handleListBuilds(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"builds": m.List()})
}

func (m *Manager) handleStartBuild(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req StartBuildRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	rec, err := m.Start(req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusAccepted, rec)
}

func (m *Manager) handleVirtioWin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"image": m.clusterVirtioWinImage()})
}

func (m *Manager) handleGetBuild(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/api/v1/builds/")
	name = strings.Trim(name, "/")
	if strings.HasSuffix(name, "/cancel") {
		m.handleCancelBuild(w, r, strings.TrimSuffix(name, "/cancel"))
		return
	}
	if name == "" {
		http.NotFound(w, r)
		return
	}
	m.mu.Lock()
	rec := m.builds[name]
	m.mu.Unlock()
	if rec == nil {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

// handleCancelBuild: POST /api/v1/builds/{disk}/cancel
func (m *Manager) handleCancelBuild(w http.ResponseWriter, r *http.Request, disk string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	rec, err := m.Cancel(disk)
	switch err {
	case nil:
		writeJSON(w, http.StatusOK, rec)
	case errNotRunning, errFinishing:
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
	default:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
}

func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
