package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config mirrors config.yml.
type Config struct {
	Server struct {
		Host       string `yaml:"host"`
		HTTPPort   int    `yaml:"http_port"`
		CORSOrigin string `yaml:"cors_origin"`
	} `yaml:"server"`

	LLM struct {
		Provider         string  `yaml:"provider"`
		Model            string  `yaml:"model"`
		APIKey           string  `yaml:"api_key"`     // key read directly from config (takes precedence)
		APIKeyEnv        string  `yaml:"api_key_env"` // fallback: env var name to read the key from
		BaseURL          string  `yaml:"base_url"`
		ToolMode         string  `yaml:"tool_mode"` // native | prompt (openai-compatible only)
		AnthropicVersion string  `yaml:"anthropic_version"`
		MaxTokens        int     `yaml:"max_tokens"`
		Temperature      float64 `yaml:"temperature"`
		MaxToolIters     int     `yaml:"max_tool_iterations"`
	} `yaml:"llm"`

	Agent struct {
		CommandTimeoutS int `yaml:"command_timeout_s"`
		OutputLimit     int `yaml:"output_limit_bytes"`
		RateLimitPerMin int    `yaml:"rate_limit_per_min"` // 0 = unlimited; else max commands/minute
		TimeBudgetMin   int    `yaml:"time_budget_min"`    // 0 = no limit; else finalize after N minutes
		WordlistsDir    string `yaml:"wordlists_dir"`      // dir of *.txt wordlists exposed to the agent
		SkillsDir       string `yaml:"skills_dir"`        // dir of SKILL.md playbooks (with index.json) the agent can search/load
	} `yaml:"agent"`

	Sandbox struct {
		Workdir string   `yaml:"workdir"`
		Shell   []string `yaml:"shell"`
	} `yaml:"sandbox"`

	MCP struct {
		Chrome struct {
			Enabled   bool     `yaml:"enabled"`
			Transport string   `yaml:"transport"` // stdio | sse | http
			URL       string   `yaml:"url"`
			Command   []string `yaml:"command"`
		} `yaml:"chrome"`
	} `yaml:"mcp"`

	Browser struct {
		Enabled    bool   `yaml:"enabled"`
		ChromePath string `yaml:"chrome_path"` // empty = auto-detect
		Headless   bool   `yaml:"headless"`
		NavWaitS   int    `yaml:"nav_wait_s"` // seconds to let the page load/XHRs fire
		TimeoutS    int    `yaml:"timeout_s"`
		RemoteURL   string `yaml:"remote_url"`    // e.g. http://localhost:9222 — attach to a real Chrome via its debug port
		AutoLaunch  bool   `yaml:"auto_launch"`   // auto-start a persistent debug Chrome if remote_url isn't up
		UserDataDir string `yaml:"user_data_dir"` // profile dir for that Chrome (log into the target there once; reused)
	} `yaml:"browser"`

	Storage struct {
		Dir string `yaml:"dir"`
	} `yaml:"storage"`

	Logging struct {
		Level    string `yaml:"level"`
		AuditLog string `yaml:"audit_log"`
	} `yaml:"logging"`

	overridePath string `yaml:"-"`
}

// APIKey resolves the LLM API key: the value in config.yml (llm.api_key) wins;
// otherwise it falls back to the environment variable named by llm.api_key_env.
func (c *Config) APIKey() string {
	if c.LLM.APIKey != "" {
		return c.LLM.APIKey
	}
	if c.LLM.APIKeyEnv == "" {
		return ""
	}
	return os.Getenv(c.LLM.APIKeyEnv)
}

// Load reads config.yml, searching a few candidate paths so the server works
// whether it is launched from the repo root or the backend/ directory.
func Load(path string) (*Config, error) {
	candidates := []string{path, "config.yml", "../config.yml", "backend/config.yml"}
	var (
		data []byte
		err  error
		used string
	)
	for _, p := range candidates {
		if p == "" {
			continue
		}
		data, err = os.ReadFile(p)
		if err == nil {
			used = p
			break
		}
	}
	if data == nil {
		return nil, fmt.Errorf("config.yml not found (looked in %v)", candidates)
	}

	var c Config
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse %s: %w", used, err)
	}

	// Overlay runtime changes saved from the Settings panel. config.yml keeps its
	// comments; config.override.yml is the machine-written layer that wins.
	c.overridePath = filepath.Join(filepath.Dir(used), "config.override.yml")
	if ov, err := os.ReadFile(c.overridePath); err == nil {
		if err := yaml.Unmarshal(ov, &c); err != nil {
			return nil, fmt.Errorf("parse %s: %w", c.overridePath, err)
		}
		fmt.Printf("[config] applied overrides from %s\n", c.overridePath)
	}

	c.applyDefaults()
	fmt.Printf("[config] loaded %s (provider=%s model=%s)\n", used, c.LLM.Provider, c.LLM.Model)
	return &c, nil
}

// SaveOverride persists the current effective config to config.override.yml so
// Settings-panel changes survive a restart. Secrets are never stored (only the
// api_key_env name is kept; the key itself lives in the environment).
func (c *Config) SaveOverride() error {
	if c.overridePath == "" {
		c.overridePath = "config.override.yml"
	}
	out, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(c.overridePath, out, 0o644)
}

func (c *Config) applyDefaults() {
	if c.Server.HTTPPort == 0 {
		c.Server.HTTPPort = 8080
	}
	if c.Server.Host == "" {
		c.Server.Host = "0.0.0.0"
	}
	if c.LLM.Provider == "" {
		c.LLM.Provider = "mock"
	}
	if c.LLM.Model == "" {
		c.LLM.Model = "claude-sonnet-4-6"
	}
	// Provider-aware base_url + tool_mode defaults.
	switch c.LLM.Provider {
	case "anthropic":
		if c.LLM.BaseURL == "" {
			c.LLM.BaseURL = "https://api.anthropic.com"
		}
	case "openrouter":
		if c.LLM.BaseURL == "" {
			c.LLM.BaseURL = "https://openrouter.ai/api/v1"
		}
		if c.LLM.ToolMode == "" {
			c.LLM.ToolMode = "native" // OpenRouter supports function-calling
		}
	case "openai":
		if c.LLM.ToolMode == "" {
			c.LLM.ToolMode = "prompt" // safe default for gateways without tool-calling
		}
	}
	if c.LLM.AnthropicVersion == "" {
		c.LLM.AnthropicVersion = "2023-06-01"
	}
	if c.LLM.MaxTokens == 0 {
		c.LLM.MaxTokens = 8192
	}
	if c.LLM.MaxToolIters == 0 {
		c.LLM.MaxToolIters = 200
	}
	if c.Agent.TimeBudgetMin == 0 {
		c.Agent.TimeBudgetMin = 60
	}
	if c.Agent.WordlistsDir == "" {
		c.Agent.WordlistsDir = "./wordlists"
	}
	if c.Agent.SkillsDir == "" {
		c.Agent.SkillsDir = "./skills"
	}
	if c.Agent.CommandTimeoutS == 0 {
		c.Agent.CommandTimeoutS = 600
	}
	if c.Agent.OutputLimit == 0 {
		c.Agent.OutputLimit = 16000
	}
	if c.Sandbox.Workdir == "" {
		c.Sandbox.Workdir = "."
	}
	if len(c.Sandbox.Shell) == 0 {
		c.Sandbox.Shell = []string{"/bin/bash", "-lc"}
	}
	if c.Storage.Dir == "" {
		c.Storage.Dir = "./data"
	}
	if c.Browser.NavWaitS == 0 {
		c.Browser.NavWaitS = 8
	}
	if c.Browser.TimeoutS == 0 {
		c.Browser.TimeoutS = 60
	}
}
