package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"terminalmcp/internal/agent"
	"terminalmcp/internal/config"
	"terminalmcp/internal/events"
	"terminalmcp/internal/model"
	"terminalmcp/internal/store"
)

type Server struct {
	cfg   *config.Config
	st    *store.Store
	hub   *events.Hub
	agent *agent.Agent
	up    websocket.Upgrader
}

func NewServer(cfg *config.Config, st *store.Store, hub *events.Hub, ag *agent.Agent) *Server {
	return &Server{
		cfg:   cfg,
		st:    st,
		hub:   hub,
		agent: ag,
		up: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true }, // dev: allow any origin
		},
	}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/config", s.handleConfig)                    // GET view, PUT update
	mux.HandleFunc("/api/logs", s.handleLogs)                        // GET audit log
	mux.HandleFunc("/api/engagements", s.handleEngagements)          // POST create, GET list
	mux.HandleFunc("/api/engagements/", s.handleEngagementByPath)    // GET/POST sub-resources
	mux.HandleFunc("/ws", s.handleWS)
	return s.withCORS(mux)
}

func (s *Server) withCORS(next http.Handler) http.Handler {
	origin := s.cfg.Server.CORSOrigin
	if origin == "" {
		origin = "*"
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":   "ok",
		"provider": s.cfg.LLM.Provider,
		"model":    s.cfg.LLM.Model,
	})
}

// ConfigView is the editable, secret-free config exposed to the Settings panel.
type ConfigView struct {
	Provider        string   `json:"provider"`
	Model           string   `json:"model"`
	BaseURL         string   `json:"base_url"`
	ToolMode        string   `json:"tool_mode"`
	APIKey          string   `json:"api_key"`     // write-only: GET returns "" (never leaks the secret); PUT sets it
	APIKeyEnv       string   `json:"api_key_env"`
	APIKeySet       bool     `json:"api_key_set"` // whether a key is configured (config or env)
	Temperature     float64  `json:"temperature"`
	MaxTokens       int      `json:"max_tokens"`
	MaxToolIters    int      `json:"max_tool_iterations"`
	CommandTimeoutS int      `json:"command_timeout_s"`
	OutputLimit     int      `json:"output_limit_bytes"`
	RateLimitPerMin int      `json:"rate_limit_per_min"`
	TimeBudgetMin   int      `json:"time_budget_min"`
	Shell           []string `json:"shell"`
	Workdir         string   `json:"workdir"`
	Chrome          struct {
		Enabled   bool     `json:"enabled"`
		Transport string   `json:"transport"`
		URL       string   `json:"url"`
		Command   []string `json:"command"`
	} `json:"chrome"`
}

func (s *Server) configView() ConfigView {
	c := s.cfg
	v := ConfigView{
		Provider:        c.LLM.Provider,
		Model:           c.LLM.Model,
		BaseURL:         c.LLM.BaseURL,
		ToolMode:        c.LLM.ToolMode,
		APIKeyEnv:       c.LLM.APIKeyEnv,
		APIKeySet:       c.APIKey() != "",
		Temperature:     c.LLM.Temperature,
		MaxTokens:       c.LLM.MaxTokens,
		MaxToolIters:    c.LLM.MaxToolIters,
		CommandTimeoutS: c.Agent.CommandTimeoutS,
		OutputLimit:     c.Agent.OutputLimit,
		RateLimitPerMin: c.Agent.RateLimitPerMin,
		TimeBudgetMin:   c.Agent.TimeBudgetMin,
		Shell:           c.Sandbox.Shell,
		Workdir:         c.Sandbox.Workdir,
	}
	v.Chrome.Enabled = c.MCP.Chrome.Enabled
	v.Chrome.Transport = c.MCP.Chrome.Transport
	v.Chrome.URL = c.MCP.Chrome.URL
	v.Chrome.Command = c.MCP.Chrome.Command
	return v
}

// GET /api/config — current settings (no secrets)
// PUT /api/config — apply + persist to config.override.yml
func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, s.configView())
	case http.MethodPut:
		var v ConfigView
		if err := json.NewDecoder(r.Body).Decode(&v); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		c := s.cfg
		c.LLM.Provider = v.Provider
		c.LLM.Model = v.Model
		c.LLM.BaseURL = v.BaseURL
		c.LLM.ToolMode = v.ToolMode
		if v.APIKeyEnv != "" {
			c.LLM.APIKeyEnv = v.APIKeyEnv
		}
		if v.APIKey != "" {
			c.LLM.APIKey = v.APIKey // set the key directly (persisted to config.override.yml)
		}
		c.LLM.Temperature = v.Temperature
		c.LLM.MaxTokens = v.MaxTokens
		c.LLM.MaxToolIters = v.MaxToolIters
		c.Agent.CommandTimeoutS = v.CommandTimeoutS
		c.Agent.OutputLimit = v.OutputLimit
		c.Agent.RateLimitPerMin = v.RateLimitPerMin
		c.Agent.TimeBudgetMin = v.TimeBudgetMin
		if len(v.Shell) > 0 {
			c.Sandbox.Shell = v.Shell
		}
		if v.Workdir != "" {
			c.Sandbox.Workdir = v.Workdir
		}
		c.MCP.Chrome.Enabled = v.Chrome.Enabled
		c.MCP.Chrome.Transport = v.Chrome.Transport
		c.MCP.Chrome.URL = v.Chrome.URL
		c.MCP.Chrome.Command = v.Chrome.Command
		if err := c.SaveOverride(); err != nil {
			writeErr(w, http.StatusInternalServerError, "save failed: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, s.configView())
	default:
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// GET /api/logs — tail of the append-only audit log (every command executed).
func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	path := s.cfg.Logging.AuditLog
	data, err := os.ReadFile(path)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"path": path, "lines": []string{}})
		return
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) > 500 {
		lines = lines[len(lines)-500:]
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": path, "lines": lines})
}

// POST /api/engagements   {name?, first_prompt}
// GET  /api/engagements
func (s *Server) handleEngagements(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var body struct {
			Name        string   `json:"name"`
			FirstPrompt string   `json:"first_prompt"`
			Goal        string   `json:"goal"`
			Cookie      string   `json:"cookie"`
			Squad       []string `json:"squad"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		if body.FirstPrompt == "" {
			writeErr(w, http.StatusBadRequest, "first_prompt is required")
			return
		}
		if body.Name == "" {
			body.Name = "Engagement " + time.Now().Format("15:04:05")
		}
		eng := &model.Engagement{
			ID:          fmt.Sprintf("e%d", time.Now().UnixNano()),
			Name:        body.Name,
			FirstPrompt: body.FirstPrompt,
			Goal:        body.Goal,
			Cookie:      body.Cookie,
			Squad:       body.Squad,
			Status:      "created",
			Phase:       "intake",
			CreatedAt:   time.Now(),
		}
		s.st.Put(eng)
		s.agent.Start(eng.ID)
		writeJSON(w, http.StatusCreated, eng)

	case http.MethodGet:
		writeJSON(w, http.StatusOK, s.st.List())

	default:
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// Handles /api/engagements/{id}, /{id}/message, /{id}/stop
func (s *Server) handleEngagementByPath(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path[len("/api/engagements/"):]
	id, action, _ := cut(path, "/")

	switch {
	case action == "" && r.Method == http.MethodGet:
		eng, err := s.st.Get(id)
		if err != nil {
			writeErr(w, http.StatusNotFound, "not found")
			return
		}
		writeJSON(w, http.StatusOK, eng)

	case action == "message" && r.Method == http.MethodPost:
		var body struct {
			Text string `json:"text"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if ok := s.agent.Reply(id, body.Text); !ok {
			writeErr(w, http.StatusConflict, "engagement is not awaiting input")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "delivered"})

	case action == "stop" && r.Method == http.MethodPost:
		s.agent.Stop(id)
		writeJSON(w, http.StatusOK, map[string]string{"status": "stopping"})

	case action == "llm" && r.Method == http.MethodGet:
		path := filepath.Join(s.cfg.Storage.Dir, id+".llm.jsonl")
		data, err := os.ReadFile(path)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"entries": []any{}})
			return
		}
		var entries []json.RawMessage
		for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
			if line != "" {
				entries = append(entries, json.RawMessage(line))
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"entries": entries})

	default:
		writeErr(w, http.StatusNotFound, "not found")
	}
}

// GET /ws?engagement={id}  — streams events; replays history on connect.
func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("engagement")
	if id == "" {
		writeErr(w, http.StatusBadRequest, "engagement query param required")
		return
	}
	conn, err := s.up.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	ch, unsub := s.hub.Subscribe(id)
	defer unsub()

	// Replay existing events so a late subscriber catches up.
	if eng, err := s.st.Get(id); err == nil {
		for _, ev := range eng.Events {
			if b, err := json.Marshal(ev); err == nil {
				_ = conn.WriteMessage(websocket.TextMessage, b)
			}
		}
	}

	// Discard client reads (also detects disconnect).
	go func() {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				_ = conn.Close()
				return
			}
		}
	}()

	for payload := range ch {
		if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
			return
		}
	}
}

// ---- helpers ----

func cut(s, sep string) (before, after string, found bool) {
	for i := 0; i+len(sep) <= len(s); i++ {
		if s[i:i+len(sep)] == sep {
			return s[:i], s[i+len(sep):], true
		}
	}
	return s, "", false
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
