package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"

	"terminalmcp/internal/agent"
	"terminalmcp/internal/api"
	"terminalmcp/internal/config"
	"terminalmcp/internal/events"
	"terminalmcp/internal/store"
	"terminalmcp/internal/terminal"
)

func main() {
	cfgPath := flag.String("config", "config.yml", "path to config.yml")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	st := store.New(cfg.Storage.Dir)
	go st.StartSaver(context.Background())
	hub := events.NewHub()
	exec := terminal.New(cfg.Sandbox.Shell, cfg.Sandbox.Workdir, cfg.Logging.AuditLog)

	if _, effective := agent.BuildProvider(cfg); effective == "mock" && cfg.LLM.Provider != "mock" {
		log.Printf("[warn] provider=%s but %s is empty; engagements will run in mock mode until a key is set", cfg.LLM.Provider, cfg.LLM.APIKeyEnv)
	} else {
		log.Printf("[llm] provider=%s model=%s base=%s tool_mode=%s", cfg.LLM.Provider, cfg.LLM.Model, cfg.LLM.BaseURL, cfg.LLM.ToolMode)
	}

	ag := agent.New(cfg, st, hub, exec)
	srv := api.NewServer(cfg, st, hub, ag)

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.HTTPPort)
	log.Printf("[server] listening on http://%s (provider=%s)", addr, cfg.LLM.Provider)
	if err := http.ListenAndServe(addr, srv.Routes()); err != nil {
		log.Fatalf("server: %v", err)
	}
}
