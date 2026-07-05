package model

import "time"

// Engagement is one pentest run, driven by the operator's first prompt.
type Engagement struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	FirstPrompt string         `json:"first_prompt"`
	Target      string         `json:"target,omitempty"` // the exact target URL/host — injected so the agent never uses placeholders
	Goal        string         `json:"goal,omitempty"`   // focused objective, e.g. "find IDOR" — drives exhaustive, goal-oriented testing
	Squad       []string       `json:"squad,omitempty"` // if set, run one parallel sub-agent per vuln class (multi-agent mode)
	Cookie      string         `json:"-"`              // session cookie for authenticated discovery (in-memory only, never persisted/returned)
	Status      string         `json:"status"` // created|running|awaiting_input|done|error|stopped
	Phase       string         `json:"phase"`
	Roadmap     []RoadmapStep  `json:"roadmap"`
	Endpoints   []string       `json:"endpoints,omitempty"` // discovered API/route requests (METHOD URL)
	Links       []string       `json:"links,omitempty"`     // discovered same-site page links
	Notes       string         `json:"notes,omitempty"`     // agent's Markdown working journal (methods tried / worked / next ideas)
	Findings    []Finding      `json:"findings"`
	Events      []Event        `json:"events"`
	Question    *OperatorAsk   `json:"question,omitempty"` // set while awaiting_input
	Report      *Report        `json:"report,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
	FinishedAt  *time.Time     `json:"finished_at,omitempty"`
}

type RoadmapStep struct {
	ID     string `json:"id"`
	Phase  string `json:"phase"`
	Title  string `json:"title"`
	Intent string `json:"intent"`
	Status string `json:"status"` // pending|running|done|skipped|failed
}

// Finding is a structured bug report. The agent fills this template via the
// add_finding tool whenever it confirms/suspects an issue.
type Finding struct {
	ID          string   `json:"id"`
	StepID      string   `json:"step_id,omitempty"`
	Title       string   `json:"title"`
	Severity    string   `json:"severity"`             // info|low|medium|high|critical
	Confidence  string   `json:"confidence,omitempty"` // confirmed|suspected|false_positive
	Type        string   `json:"type,omitempty"`       // IDOR, XSS, SQLi, SSRF, ...
	CWE         string   `json:"cwe,omitempty"`        // e.g. CWE-639
	OWASP       string   `json:"owasp,omitempty"`      // e.g. A01:2021-Broken Access Control
	CVSS        string   `json:"cvss,omitempty"`       // score and/or vector
	Target      string   `json:"target"`
	Endpoint    string   `json:"endpoint,omitempty"`  // affected URL/route
	Parameter   string   `json:"parameter,omitempty"` // affected parameter
	Description string   `json:"description,omitempty"`
	Impact      string   `json:"impact,omitempty"`
	Steps       []string `json:"steps_to_reproduce,omitempty"`
	Request     string   `json:"request,omitempty"`  // raw HTTP request used
	Response    string   `json:"response,omitempty"` // evidence response snippet
	Evidence    string   `json:"evidence,omitempty"`
	Remediation string   `json:"remediation,omitempty"`
	References  []string `json:"references,omitempty"`
}

type OperatorAsk struct {
	Prompt    string   `json:"prompt"`
	Questions []string `json:"questions"`
	Options   []string `json:"options,omitempty"` // if set, UI shows these as buttons (e.g. retry/skip)
}

type Report struct {
	ExecutiveSummary string   `json:"executive_summary"`
	Methodology      string   `json:"methodology"`
	Scope            string   `json:"scope"`
	GeneratedAt      time.Time `json:"generated_at"`
}

// Event is an append-only record streamed to the UI over WebSocket.
type Event struct {
	Type      string      `json:"type"`
	StepID    string      `json:"step_id,omitempty"`
	Data      interface{} `json:"data,omitempty"`
	Timestamp time.Time   `json:"ts"`
}

// Event type constants.
const (
	EvStatus         = "status"
	EvPhaseChanged   = "phase_changed"
	EvRoadmapUpdated = "roadmap_updated"
	EvStepBegin      = "step_begin"
	EvCommandStarted = "command_started"
	EvOutputChunk    = "output_chunk"
	EvCommandDone    = "command_finished"
	EvAnalysis       = "analysis"
	EvFinding        = "finding"
	EvRecommendation = "recommendation"
	EvAgentMessage   = "agent_message"
	EvAwaitingInput  = "awaiting_input"
	EvReport         = "report_finalized"
	EvError          = "error"
)
