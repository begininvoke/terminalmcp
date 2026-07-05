package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"terminalmcp/internal/browser"
	"terminalmcp/internal/config"
	"terminalmcp/internal/events"
	"terminalmcp/internal/llm"
	"terminalmcp/internal/model"
	"terminalmcp/internal/store"
	"terminalmcp/internal/terminal"
)

// Agent orchestrates the LLM tool-use loop against the terminal executor.
type Agent struct {
	cfg  *config.Config
	st   *store.Store
	hub  *events.Hub
	exec *terminal.Executor

	mu      sync.Mutex
	replies map[string]chan string        // engagementID -> operator reply channel
	cancels map[string]context.CancelFunc // engagementID -> stop

	rlMu   sync.Mutex // rate limiter
	rlNext time.Time

	cmdMu   sync.Mutex     // repeated-command guard
	cmdSeen map[string]int // engagementID\x00command -> times run

	autonomous map[string]bool // engagementID -> skip operator intake (squad mode)
	memoryMu   sync.Mutex      // guards the global memory.md file
	llmSem     chan struct{}   // caps concurrent LLM calls (protects the gateway under squad mode)
}

func New(cfg *config.Config, st *store.Store, hub *events.Hub, exec *terminal.Executor) *Agent {
	return &Agent{
		cfg:        cfg,
		st:         st,
		hub:        hub,
		exec:       exec,
		replies:    make(map[string]chan string),
		cancels:    make(map[string]context.CancelFunc),
		cmdSeen:    make(map[string]int),
		autonomous: make(map[string]bool),
		llmSem:     make(chan struct{}, 2), // at most 2 concurrent LLM calls
	}
}

// BuildProvider constructs the LLM provider from current config. Returns
// (nil, "mock") when no usable provider/key is configured, so the agent runs the
// scripted demo instead.
func BuildProvider(cfg *config.Config) (llm.Provider, string) {
	switch cfg.LLM.Provider {
	case "anthropic":
		if key := cfg.APIKey(); key != "" {
			return llm.NewAnthropic(key, cfg.LLM.Model, cfg.LLM.BaseURL, cfg.LLM.AnthropicVersion, cfg.LLM.MaxTokens, cfg.LLM.Temperature), "anthropic"
		}
	case "openai", "openrouter":
		if key := cfg.APIKey(); key != "" {
			return llm.NewOpenAI(key, cfg.LLM.Model, cfg.LLM.BaseURL, cfg.LLM.ToolMode, cfg.LLM.MaxTokens, cfg.LLM.Temperature), cfg.LLM.Provider
		}
	}
	return nil, "mock"
}

func rawSchema(s string) json.RawMessage { return json.RawMessage(s) }

// Start launches the agent loop for an engagement in a goroutine.
func (a *Agent) Start(engagementID string) {
	ctx, cancel := context.WithCancel(context.Background())
	a.mu.Lock()
	a.cancels[engagementID] = cancel
	a.replies[engagementID] = make(chan string, 1)
	a.mu.Unlock()

	provider, _ := BuildProvider(a.cfg)

	go func() {
		defer cancel()
		a.setStatus(engagementID, "running")
		var err error
		if provider == nil {
			if a.cfg.LLM.Provider == "mock" {
				err = a.runMock(ctx, engagementID) // explicit demo mode
			} else {
				// Provider is real but no key — fail loudly instead of silently
				// running the scripted demo (which previously looked like a real test).
				a.emit(engagementID, model.Event{Type: model.EvError, Data: map[string]string{
					"message": "API key not set for provider '" + a.cfg.LLM.Provider + "'. Export " + a.cfg.LLM.APIKeyEnv +
						" and restart the backend, or set provider to 'mock' in Settings to run the scripted demo.",
				}})
				a.setStatus(engagementID, "error")
				return
			}
		} else if eng, e := a.st.Get(engagementID); e == nil && len(eng.Squad) > 0 {
			err = a.runSquad(ctx, engagementID, provider, eng)
		} else {
			err = a.runLLM(ctx, engagementID, provider)
		}
		if err != nil && ctx.Err() == nil {
			a.emit(engagementID, model.Event{Type: model.EvError, Data: map[string]string{"message": err.Error()}})
			a.setStatus(engagementID, "error")
			return
		}
		if ctx.Err() != nil {
			a.setStatus(engagementID, "stopped")
			return
		}
		// Honest terminal status: "completed" if the roadmap is fully worked through
		// (or a squad finished all its agents); otherwise "incomplete".
		squad := false
		if e, gerr := a.st.Get(engagementID); gerr == nil {
			squad = len(e.Squad) > 0
		}
		if squad || a.engagementComplete(engagementID) {
			a.setStatus(engagementID, "completed")
		} else {
			a.setStatus(engagementID, "incomplete")
		}
	}()
}

// appendNote adds a Markdown bullet to the engagement's working journal,
// persists it on the engagement, and mirrors it to data/<id>.notes.md.
func (a *Agent) appendNote(id, status, text string) {
	status = strings.ToUpper(strings.TrimSpace(status))
	if status == "" {
		status = "NOTE"
	}
	ts := time.Now().Format("15:04:05")
	bullet := fmt.Sprintf("- [%s] **%s** %s\n", ts, status, strings.TrimSpace(text))

	var full string
	_ = a.st.Update(id, func(e *model.Engagement) {
		if e.Notes == "" {
			e.Notes = "# Working journal\n\n"
		}
		e.Notes += bullet
		full = e.Notes
	})
	if dir := a.cfg.Storage.Dir; dir != "" && full != "" {
		_ = os.WriteFile(filepath.Join(dir, id+".notes.md"), []byte(full), 0o644)
	}
	a.emit(id, model.Event{Type: model.EvAgentMessage, Data: map[string]string{"text": "📝 " + status + ": " + truncURL(text)}})
}

// rememberGlobal appends a durable lesson to the cross-engagement memory.md.
func (a *Agent) rememberGlobal(id, text string) {
	dir := a.cfg.Storage.Dir
	text = strings.TrimSpace(text)
	if dir == "" || text == "" {
		return
	}
	a.memoryMu.Lock()
	f, err := os.OpenFile(filepath.Join(dir, "memory.md"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err == nil {
		fmt.Fprintf(f, "- %s\n", text)
		f.Close()
	}
	a.memoryMu.Unlock()
	a.emit(id, model.Event{Type: model.EvAgentMessage, Data: map[string]string{"text": "🧠 remembered: " + truncURL(text)}})
}

// globalMemorySection returns the long-term memory to inject at engagement start.
func (a *Agent) globalMemorySection() string {
	dir := a.cfg.Storage.Dir
	if dir == "" {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(dir, "memory.md"))
	if err != nil || len(b) == 0 {
		return ""
	}
	mem := string(b)
	if len(mem) > 3000 {
		mem = mem[len(mem)-3000:]
	}
	return "\n\n=== LONG-TERM MEMORY (lessons from past engagements; use if relevant) ===\n" + mem
}

// notesSection returns the journal to inject into the prompt each turn (tail-capped).
func (a *Agent) notesSection(id string) string {
	eng, err := a.st.Get(id)
	if err != nil || strings.TrimSpace(eng.Notes) == "" {
		return ""
	}
	notes := eng.Notes
	if len(notes) > 4000 { // keep the most recent journal entries
		notes = "...(earlier journal trimmed)...\n" + notes[len(notes)-4000:]
	}
	return "\n\n=== YOUR WORKING JOURNAL (review before acting; do NOT repeat FAILED methods — invent a new one) ===\n" + notes
}

var placeholderHosts = []string{
	"target-url.com", "target.example.com", "www.target-url.com", "your-target.com",
	"yourdomain.com", "your-domain.com", "target.com", "example.com", "www.example.com",
	"example.org", "test.com", "victim.com", "target_url", "TARGET_URL",
}

// targetHint injects the exact target so the agent never invents placeholders.
func (a *Agent) targetHint(id string) string {
	eng, err := a.st.Get(id)
	if err != nil || eng.Target == "" {
		return ""
	}
	return "\n\n=== PRIMARY TARGET ===\nThe ONLY in-scope target is: " + eng.Target + "\n" +
		"Use this EXACT host/URL in every command, browser_capture, and tool call. " +
		"NEVER use placeholder domains like target-url.com, example.com, or target.example.com — those are wrong. " +
		"If you need the host, it is: " + targetHost(eng.Target) + "."
}

func targetHost(raw string) string {
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		return u.Host
	}
	return raw
}

// fixTarget rewrites placeholder hosts in a command/URL to the real target host.
func (a *Agent) fixTarget(id, s string) (string, bool) {
	eng, err := a.st.Get(id)
	if err != nil || eng.Target == "" {
		return s, false
	}
	host := targetHost(eng.Target)
	if host == "" {
		return s, false
	}
	out, changed := s, false
	for _, p := range placeholderHosts {
		if strings.Contains(out, p) {
			out = strings.ReplaceAll(out, p, host)
			changed = true
		}
	}
	return out, changed
}

// wordlistHint lists the installed wordlists with absolute paths so the model
// uses real -w paths instead of guessing /usr/share/wordlists or /path/to/....
func (a *Agent) wordlistHint() string {
	dir := a.cfg.Agent.WordlistsDir
	if dir == "" {
		return ""
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	abs, _ := filepath.Abs(dir)
	var lines []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".txt") {
			lines = append(lines, "- "+filepath.Join(abs, e.Name()))
		}
	}
	if len(lines) == 0 {
		return ""
	}
	return "\n\nAVAILABLE WORDLISTS — use these EXACT paths for ffuf/gobuster -w. Do NOT invent paths like " +
		"/usr/share/wordlists/... or /path/to/... (they do not exist on this host):\n" + strings.Join(lines, "\n") +
		"\nIf you need a payload list (xss/sqli) for FUZZ, use the matching file above. For content discovery use common.txt."
}

// goalMet reports whether the focused goal looks achieved — i.e. there is a
// CONFIRMED finding matching the goal (so the agent keeps going until it both
// finds AND confirms/exploits the bug, not just suspects it).
func (a *Agent) goalMet(id, goal string) bool {
	eng, err := a.st.Get(id)
	if err != nil {
		return false
	}
	g := strings.ToLower(goal)
	for _, f := range eng.Findings {
		if f.Confidence != "confirmed" {
			continue
		}
		// If the goal names a class, require a matching confirmed finding;
		// otherwise any confirmed finding satisfies it.
		if strings.Contains(g, "idor") || strings.Contains(g, "bola") {
			if strings.Contains(strings.ToLower(f.Type+" "+f.Title), "idor") || strings.Contains(strings.ToLower(f.Type+" "+f.Title), "bola") {
				return true
			}
		} else {
			return true
		}
	}
	return false
}

// engagementComplete reports whether every roadmap step is done or skipped.
func (a *Agent) engagementComplete(id string) bool {
	eng, err := a.st.Get(id)
	if err != nil || len(eng.Roadmap) == 0 {
		return false
	}
	for _, s := range eng.Roadmap {
		if s.Status != "done" && s.Status != "skipped" {
			return false
		}
	}
	return true
}

// Reply delivers an operator answer to a paused (awaiting_input) engagement.
func (a *Agent) Reply(engagementID, text string) bool {
	a.mu.Lock()
	ch := a.replies[engagementID]
	a.mu.Unlock()
	if ch == nil {
		return false
	}
	select {
	case ch <- text:
		return true
	default:
		return false
	}
}

// Stop cancels a running engagement.
func (a *Agent) Stop(engagementID string) {
	a.mu.Lock()
	cancel := a.cancels[engagementID]
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// ---- LLM loop ----

// runSquad fans out one focused sub-agent per vuln class. All sub-agents share
// the same engagement (endpoints, findings, journal, memory) and run in parallel,
// bounded by the global rate limit and time budget.
func (a *Agent) runSquad(ctx context.Context, id string, provider llm.Provider, eng *model.Engagement) error {
	a.mu.Lock()
	a.autonomous[id] = true // squad runs without operator intake
	a.mu.Unlock()

	classes := eng.Squad
	a.emit(id, model.Event{Type: model.EvAgentMessage, Data: map[string]string{
		"text": fmt.Sprintf("🤝 Squad mode: launching %d parallel agents — %s", len(classes), strings.Join(classes, ", "))}})

	var wg sync.WaitGroup
	for _, class := range classes {
		wg.Add(1)
		go func(class string) {
			defer wg.Done()
			defer func() { _ = recover() }() // one agent crashing must not kill the squad
			a.emit(id, model.Event{Type: model.EvAgentMessage, Data: map[string]string{"text": "[" + class + "] agent started"}})
			prompt := fmt.Sprintf(
				"You are the %q specialist in a parallel squad testing this engagement. Scope is AUTHORIZED — do NOT ask for "+
					"confirmation, proceed immediately. Focus ONLY on finding and CONFIRMING %s vulnerabilities. Endpoints discovered "+
					"by the squad are shared (run browser_capture if none exist yet). Use search_skills for a %s playbook, journal your "+
					"work with add_note, and record confirmed findings. Original brief:\n%s",
				class, class, class, eng.FirstPrompt)
			if err := a.runAgentLoop(ctx, id, provider, class, class, prompt); err != nil && ctx.Err() == nil {
				a.emit(id, model.Event{Type: model.EvAgentMessage, Data: map[string]string{"text": "[" + class + "] agent error: " + err.Error()}})
			}
			a.emit(id, model.Event{Type: model.EvAgentMessage, Data: map[string]string{"text": "[" + class + "] agent finished"}})
		}(class)
	}
	wg.Wait()

	if ctx.Err() != nil {
		return ctx.Err()
	}
	fcount := 0
	if e, err := a.st.Get(id); err == nil {
		fcount = len(e.Findings)
	}
	a.emit(id, model.Event{Type: model.EvAgentMessage, Data: map[string]string{
		"text": fmt.Sprintf("🤝 Squad complete — %d agents finished, %d findings recorded.", len(classes), fcount)}})
	return nil
}

// runLLM runs the engagement's own goal/prompt.
func (a *Agent) runLLM(ctx context.Context, id string, provider llm.Provider) error {
	eng, err := a.st.Get(id)
	if err != nil {
		return err
	}
	return a.runAgentLoop(ctx, id, provider, eng.Goal, "", eng.FirstPrompt)
}

// runAgentLoop is the core ReAct loop. goal/firstPrompt can be overridden so a
// squad can run several focused sub-agents against the same shared engagement;
// label prefixes this sub-agent's narration in the UI.
func (a *Agent) runAgentLoop(ctx context.Context, id string, provider llm.Provider, goal, label, firstPrompt string) error {
	tag := ""
	if label != "" {
		tag = "[" + label + "] "
	}
	messages := []llm.Message{{
		Role:    "user",
		Content: []llm.Block{{Type: "text", Text: firstPrompt}},
	}}
	tools := toolDefs()
	nudges := 0
	maxNudges := 3
	// A focused goal makes the agent exhaustive: keep going (bounded by the time
	// budget) instead of stopping after a few nudges.
	baseSys := systemPrompt + a.targetHint(id) + a.wordlistHint() + a.skillHint() + a.globalMemorySection()
	if goal != "" {
		baseSys += goalDirective(goal) + a.autoSkill(goal)
		maxNudges = 1 << 30 // effectively unlimited; the time budget governs
	}

	start := time.Now()
	budget := time.Duration(a.cfg.Agent.TimeBudgetMin) * time.Minute

	for i := 0; i < a.cfg.LLM.MaxToolIters; i++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if budget > 0 && time.Since(start) > budget {
			a.emit(id, model.Event{Type: model.EvAgentMessage, Data: map[string]string{
				"text": fmt.Sprintf("Time budget (%d min) reached — finalizing.", a.cfg.Agent.TimeBudgetMin)}})
			return nil
		}
		trimOldToolResults(messages, 8)
		// Re-inject the working journal each turn so the agent always "checks the md".
		sys := baseSys + a.notesSection(id)
		a.llmSem <- struct{}{} // cap concurrent gateway calls (squad-safe)
		resp, err := provider.Create(ctx, sys, messages, tools)
		<-a.llmSem
		a.logLLM(id, sys, messages, resp, err)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			// Don't kill the engagement on an LLM failure — pause and let the
			// operator retry (e.g. after fixing a proxy/key) or skip.
			decision := a.awaitDecision(ctx, id, "LLM request failed: "+err.Error())
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if strings.Contains(strings.ToLower(decision), "retry") {
				continue // re-issue the same request
			}
			return nil // skip -> end gracefully (engagement marked incomplete)
		}
		messages = append(messages, llm.Message{Role: "assistant", Content: resp.Content})

		// Surface any assistant prose to the UI.
		var text strings.Builder
		var toolUses []llm.Block
		for _, b := range resp.Content {
			switch b.Type {
			case "text":
				text.WriteString(b.Text)
			case "tool_use":
				toolUses = append(toolUses, b)
			}
		}
		if s := strings.TrimSpace(text.String()); s != "" {
			a.emit(id, model.Event{Type: model.EvAgentMessage, Data: map[string]string{"text": tag + s}})
		}

		if resp.StopReason != "tool_use" || len(toolUses) == 0 {
			// The model ended its turn. Decide whether to push it to keep going.
			goalActive := goal != "" && !a.goalMet(id, goal)
			done := a.engagementComplete(id) && !goalActive
			if done || nudges >= maxNudges {
				return nil
			}
			nudges++
			var nudge string
			if goalActive {
				strategy := nextStrategy(nudges)
				a.emit(id, model.Event{Type: model.EvAgentMessage, Data: map[string]string{
					"text": fmt.Sprintf("%sGoal '%s' not found yet — switching method (attempt %d): %s", tag, goal, nudges, truncURL(strategy))}})
				nudge = fmt.Sprintf("Your PRIMARY GOAL (%s) is NOT confirmed yet. Do NOT finalize and do NOT repeat what already failed. "+
					"CHANGE YOUR APPROACH now — attempt %d, a DIFFERENT method:\n%s\n"+
					"After trying it, if you find a candidate, CONFIRM it (for IDOR: idor_scan with two account cookies; otherwise a clean curl reproduction) "+
					"and record the finding with confidence=confirmed. Then keep rotating methods until the goal is confirmed.", goal, nudges, strategy)
			} else {
				a.emit(id, model.Event{Type: model.EvAgentMessage, Data: map[string]string{
					"text": "Roadmap not complete — prompting the agent to continue."}})
				nudge = "You ended your turn, but the engagement is NOT finished: there are pending roadmap steps. " +
					"Continue now — begin_step the next pending step and run the required commands. Mark steps you intentionally " +
					"skip as 'skipped'. Only call finalize_report when every step is done or skipped. Respond with your next tool action."
			}
			messages = append(messages, llm.Message{Role: "user", Content: []llm.Block{{Type: "text", Text: nudge}}})
			continue
		}

		// Execute each tool call and feed results back.
		var results []llm.Block
		for _, tu := range toolUses {
			out := a.execTool(ctx, id, tu.Name, tu.Input)
			results = append(results, llm.Block{
				Type:      "tool_result",
				ToolUseID: tu.ID,
				Content:   out,
			})
		}
		messages = append(messages, llm.Message{Role: "user", Content: results})
	}
	// Hitting the iteration cap is not a fatal error — finalize gracefully with
	// whatever was found (engagement ends "incomplete", findings preserved).
	a.emit(id, model.Event{Type: model.EvAgentMessage, Data: map[string]string{
		"text": fmt.Sprintf("Reached the max tool iterations (%d) — finalizing with findings so far.", a.cfg.LLM.MaxToolIters)}})
	return nil
}

// trimOldToolResults caps context growth: tool results older than the most
// recent keepRecent messages are truncated, since the model rarely needs full
// old command output and large outputs otherwise blow past the context limit.
// Message structure (tool_use/tool_result pairing) is preserved.
func trimOldToolResults(messages []llm.Message, keepRecent int) {
	cut := len(messages) - keepRecent
	for i := 0; i < cut; i++ {
		for j := range messages[i].Content {
			b := &messages[i].Content[j]
			if b.Type == "tool_result" && len(b.Content) > 500 {
				b.Content = b.Content[:500] + "\n...[older output trimmed to save context]"
			}
		}
	}
}

// execTool runs one tool call and returns a short result string for the model.
func (a *Agent) execTool(ctx context.Context, id, name string, input json.RawMessage) string {
	switch name {
	case "run_command":
		var in struct {
			Command  string `json:"command"`
			TimeoutS int    `json:"timeout_s"`
		}
		_ = json.Unmarshal(input, &in)
		return a.runCommand(ctx, id, in.Command, in.TimeoutS)

	case "browser_capture":
		var in struct {
			URL    string `json:"url"`
			Cookie string `json:"cookie"`
			WaitS  int    `json:"wait_s"`
		}
		_ = json.Unmarshal(input, &in)
		return a.runBrowserCapture(ctx, id, in.URL, in.Cookie, in.WaitS)

	case "idor_scan":
		var in struct {
			Endpoints []string `json:"endpoints"`
			CookieA   string   `json:"cookie_a"`
			CookieB   string   `json:"cookie_b"`
			Max       int      `json:"max"`
		}
		_ = json.Unmarshal(input, &in)
		return a.runIdorScan(ctx, id, in.Endpoints, in.CookieA, in.CookieB, in.Max)

	case "update_roadmap":
		var in struct {
			Steps []model.RoadmapStep `json:"steps"`
		}
		_ = json.Unmarshal(input, &in)
		_ = a.st.Update(id, func(e *model.Engagement) { e.Roadmap = in.Steps })
		a.emit(id, model.Event{Type: model.EvRoadmapUpdated, Data: map[string]any{"steps": in.Steps}})
		return "roadmap updated"

	case "begin_step":
		var in struct {
			StepID    string `json:"step_id"`
			Title     string `json:"title"`
			Rationale string `json:"rationale"`
		}
		_ = json.Unmarshal(input, &in)
		a.setStepStatus(id, in.StepID, "running")
		a.emit(id, model.Event{Type: model.EvStepBegin, StepID: in.StepID, Data: map[string]string{
			"title": in.Title, "rationale": in.Rationale,
		}})
		return "step started"

	case "record_analysis":
		var in struct {
			StepID  string `json:"step_id"`
			Summary string `json:"summary"`
			Risk    string `json:"risk"`
		}
		_ = json.Unmarshal(input, &in)
		a.setStepStatus(id, in.StepID, "done")
		a.emit(id, model.Event{Type: model.EvAnalysis, StepID: in.StepID, Data: map[string]string{
			"summary": in.Summary, "risk": in.Risk,
		}})
		return "analysis recorded"

	case "search_skills":
		var in struct {
			Query string `json:"query"`
			Limit int    `json:"limit"`
		}
		_ = json.Unmarshal(input, &in)
		matches := a.searchSkills(in.Query, in.Limit)
		if len(matches) == 0 {
			return "No skills matched '" + in.Query + "'. Try broader keywords."
		}
		var sb strings.Builder
		sb.WriteString("Matching skill playbooks (load one with load_skill):\n")
		for _, s := range matches {
			sb.WriteString("- " + s.Name + " — " + s.Description + "\n")
		}
		return sb.String()

	case "load_skill":
		var in struct {
			Name string `json:"name"`
		}
		_ = json.Unmarshal(input, &in)
		body, err := a.loadSkillBody(in.Name)
		if err != nil {
			return "could not load skill '" + in.Name + "': " + err.Error() + " (use search_skills to find the exact name)"
		}
		a.emit(id, model.Event{Type: model.EvAgentMessage, Data: map[string]string{"text": "📖 loaded skill: " + in.Name}})
		if len(body) > a.cfg.Agent.OutputLimit {
			body = body[:a.cfg.Agent.OutputLimit] + "\n...[truncated]"
		}
		return body

	case "remember":
		var in struct {
			Text string `json:"text"`
		}
		_ = json.Unmarshal(input, &in)
		a.rememberGlobal(id, in.Text)
		return "saved to long-term memory"

	case "add_note":
		var in struct {
			Status string `json:"status"`
			Text   string `json:"text"`
		}
		_ = json.Unmarshal(input, &in)
		a.appendNote(id, in.Status, in.Text)
		return "noted"

	case "add_finding":
		var f model.Finding
		_ = json.Unmarshal(input, &f)
		// IDOR/BOLA can only be marked "confirmed" by idor_scan's cross-account
		// test — not by the model's say-so. Downgrade self-confirmed claims.
		if t := strings.ToLower(f.Type + " " + f.Title); (strings.Contains(t, "idor") || strings.Contains(t, "bola") || strings.Contains(t, "access control")) && f.Confidence == "confirmed" {
			f.Confidence = "suspected"
			f.Evidence = strings.TrimSpace(f.Evidence + "\n[confidence set to 'suspected': IDOR is only confirmed by idor_scan's cross-account A/B/unauth diff, not self-assessment.]")
			a.recordFinding(id, f)
			return "finding recorded (confidence downgraded to 'suspected' — confirm IDOR via idor_scan with two account cookies: cookie_a and cookie_b)"
		}
		a.recordFinding(id, f)
		return "finding recorded"

	case "recommend":
		var in struct {
			StepID      string `json:"step_id"`
			NextActions []struct {
				Title string `json:"title"`
				Why   string `json:"why"`
			} `json:"next_actions"`
		}
		_ = json.Unmarshal(input, &in)
		a.emit(id, model.Event{Type: model.EvRecommendation, StepID: in.StepID, Data: map[string]any{
			"next_actions": in.NextActions,
		}})
		return "recommendations recorded"

	case "ask_operator":
		var in struct {
			Prompt    string   `json:"prompt"`
			Questions []string `json:"questions"`
		}
		_ = json.Unmarshal(input, &in)
		return a.askOperator(ctx, id, in.Prompt, in.Questions)

	case "finalize_report":
		var r model.Report
		_ = json.Unmarshal(input, &r)
		r.GeneratedAt = time.Now()
		_ = a.st.Update(id, func(e *model.Engagement) { e.Report = &r })
		a.emit(id, model.Event{Type: model.EvReport, Data: r})
		return "report finalized"
	}
	return "unknown tool: " + name
}

// rateLimitWait enforces agent.rate_limit_per_min across all commands. It reads
// the limit from config on each call so Settings changes take effect immediately.
func (a *Agent) rateLimitWait(ctx context.Context, id string) {
	r := a.cfg.Agent.RateLimitPerMin
	if r <= 0 {
		return
	}
	interval := time.Minute / time.Duration(r)
	a.rlMu.Lock()
	now := time.Now()
	start := now
	if a.rlNext.After(now) {
		start = a.rlNext
	}
	wait := start.Sub(now)
	a.rlNext = start.Add(interval)
	a.rlMu.Unlock()

	if wait > 0 {
		a.emit(id, model.Event{Type: model.EvAgentMessage, Data: map[string]string{
			"text": fmt.Sprintf("rate limit (%d/min): waiting %s before next command", r, wait.Round(time.Millisecond)),
		}})
		select {
		case <-ctx.Done():
		case <-time.After(wait):
		}
	}
}

var (
	urlTokenRe = regexp.MustCompile(`https?://\S+`)
	wordlistRe = regexp.MustCompile(`(-w|--wordlist)\s+(\S+)`)
)

func fileExists(p string) bool {
	if p == "" {
		return false
	}
	_, err := os.Stat(p)
	return err == nil
}

// sanitizeCommand repairs the two failure modes the model keeps hitting:
//  1. an unquoted URL containing '&'/'?' (bash backgrounds/splits it),
//  2. a -w wordlist path that doesn't exist (Kali/invented paths).
// Returns the fixed command and a human note (empty if nothing changed).
func (a *Agent) sanitizeCommand(command string) (string, string) {
	var notes []string
	out := command

	// 1) Quote bare URLs with shell-significant chars.
	out = quoteBareURLs(out)
	if out != command {
		notes = append(notes, "quoted URL so '&'/'?' aren't shell operators")
	}

	// 2) Fix missing wordlist paths.
	before := out
	out = wordlistRe.ReplaceAllStringFunc(out, func(m string) string {
		sub := wordlistRe.FindStringSubmatch(m)
		flag, tok := sub[1], sub[2]
		unq := strings.Trim(tok, "'\"")
		path, kw := unq, ""
		if i := strings.LastIndex(unq, ":"); i > 0 && !strings.ContainsAny(unq[i+1:], "/.") {
			path, kw = unq[:i], unq[i:] // wordlist:KEYWORD form
		}
		if fileExists(path) {
			return m
		}
		real := a.pickWordlist(command)
		if real == "" {
			return m
		}
		notes = append(notes, "wordlist "+path+" → "+filepath.Base(real))
		return flag + " " + real + kw
	})
	_ = before

	if len(notes) == 0 {
		return command, ""
	}
	return out, strings.Join(notes, "; ")
}

// quoteBareURLs single-quotes unquoted http(s) URLs that contain '&' or '?'
// (and no shell pipe/redirect), so bash treats them as one argument.
func quoteBareURLs(cmd string) string {
	locs := urlTokenRe.FindAllStringIndex(cmd, -1)
	if locs == nil {
		return cmd
	}
	var b strings.Builder
	prev := 0
	for _, loc := range locs {
		s, e := loc[0], loc[1]
		tok := cmd[s:e]
		b.WriteString(cmd[prev:s])
		alreadyQuoted := s > 0 && (cmd[s-1] == '\'' || cmd[s-1] == '"')
		if !alreadyQuoted && strings.ContainsAny(tok, "&?") && !strings.ContainsAny(tok, "'|;`<>") {
			b.WriteString("'" + tok + "'")
		} else {
			b.WriteString(tok)
		}
		prev = e
	}
	b.WriteString(cmd[prev:])
	return b.String()
}

// pickWordlist chooses a real wordlist based on the command's intent.
func (a *Agent) pickWordlist(cmd string) string {
	dir := a.cfg.Agent.WordlistsDir
	if dir == "" {
		return ""
	}
	abs, _ := filepath.Abs(dir)
	l := strings.ToLower(cmd)
	name := "common.txt"
	switch {
	case strings.Contains(l, "xss"):
		name = "xss.txt"
	case strings.Contains(l, "sql"):
		name = "sqli.txt"
	case strings.Contains(l, "param"):
		name = "params.txt"
	}
	if p := filepath.Join(abs, name); fileExists(p) {
		return p
	}
	if p := filepath.Join(abs, "common.txt"); fileExists(p) {
		return p
	}
	return ""
}

// cookieFlagRe detects a cookie already supplied on the command line (curl -b,
// --cookie), so we don't double up when injecting the session cookie.
var cookieFlagRe = regexp.MustCompile(`(^|\s)(-b|--cookie)(\s|=)`)

// injectSessionCookie appends the engagement's session cookie to curl/ffuf/wget
// commands that hit the in-scope target, so terminal scans run authenticated with
// the same login browser_capture discovered. It is a no-op when there is no
// cookie, the command doesn't reference the target host, or a cookie is already
// present. Returns the rewritten command and a human note (empty if unchanged).
func (a *Agent) injectSessionCookie(id, command string) (string, string) {
	eng, err := a.st.Get(id)
	if err != nil || strings.TrimSpace(eng.Cookie) == "" {
		return command, ""
	}
	host := targetHost(eng.Target)
	if host == "" || !strings.Contains(command, host) {
		return command, "" // only ever attach the session to the in-scope target
	}
	if strings.Contains(strings.ToLower(command), "cookie:") || cookieFlagRe.MatchString(command) {
		return command, "" // command already carries a cookie — respect it
	}
	cookie := strings.ReplaceAll(eng.Cookie, `'`, `'\''`) // safe inside single quotes
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return command, ""
	}
	switch strings.ToLower(fields[0]) {
	case "curl":
		return command + " -b '" + cookie + "'", "attached session cookie (-b) so curl runs authenticated"
	case "ffuf":
		return command + " -H 'Cookie: " + cookie + "'", "attached session cookie header so ffuf runs authenticated"
	case "wget":
		return command + " --header='Cookie: " + cookie + "'", "attached session cookie so wget runs authenticated"
	}
	return command, ""
}

// redactCookie masks the session cookie value in a string before it is emitted to
// the UI / persisted event log, keeping the secret out of stored data.
func (a *Agent) redactCookie(id, s string) string {
	eng, err := a.st.Get(id)
	if err != nil || strings.TrimSpace(eng.Cookie) == "" {
		return s
	}
	out := strings.ReplaceAll(s, eng.Cookie, "<session-cookie>")
	escaped := strings.ReplaceAll(eng.Cookie, `'`, `'\''`)
	return strings.ReplaceAll(out, escaped, "<session-cookie>")
}

// runCommand executes through the terminal layer with live streaming.
func (a *Agent) runCommand(ctx context.Context, id, command string, timeoutS int) string {
	// Replace placeholder hosts (target-url.com, example.com, …) with the real target.
	if fixed, changed := a.fixTarget(id, command); changed {
		command = fixed
		a.emit(id, model.Event{Type: model.EvAgentMessage, Data: map[string]string{"text": "↳ rewrote placeholder host to the real target"}})
	}
	// Auto-repair common mistakes before running: quote bare URLs (so '&' isn't
	// treated as a bash background operator) and swap non-existent wordlist paths
	// for a real one. Saves the agent from looping on broken commands.
	if fixed, note := a.sanitizeCommand(command); note != "" {
		command = fixed
		a.emit(id, model.Event{Type: model.EvAgentMessage, Data: map[string]string{"text": "auto-fixed command — " + note}})
	}

	// Attach the engagement's session cookie to curl/ffuf/wget hitting the target,
	// so terminal scans run authenticated (same login browser_capture discovered).
	if fixed, note := a.injectSessionCookie(id, command); note != "" {
		command = fixed
		a.emit(id, model.Event{Type: model.EvAgentMessage, Data: map[string]string{"text": note}})
	}

	// Guard against the model looping on the same (often failing) command.
	key := id + "\x00" + command
	a.cmdMu.Lock()
	a.cmdSeen[key]++
	n := a.cmdSeen[key]
	a.cmdMu.Unlock()
	if n > 3 {
		a.emit(id, model.Event{Type: model.EvAgentMessage, Data: map[string]string{
			"text": "Blocked a repeated command (run " + fmt.Sprint(n-1) + " times already): " + truncURL(command),
		}})
		return "exit_code=-1\nREFUSED: you have already run this exact command " + fmt.Sprint(n-1) +
			" times and it is not working. Do NOT repeat it. Fix it (e.g. a wrong wordlist path / missing file) or move on to the next step."
	}

	a.rateLimitWait(ctx, id)
	cid := fmt.Sprintf("c%d", time.Now().UnixNano())
	a.emit(id, model.Event{Type: model.EvCommandStarted, Data: map[string]string{"cid": cid, "cmdline": a.redactCookie(id, command)}})

	timeout := time.Duration(a.cfg.Agent.CommandTimeoutS) * time.Second
	if timeoutS > 0 {
		timeout = time.Duration(timeoutS) * time.Second
	}
	res := a.exec.Run(ctx, command, timeout, func(stream, line string) {
		a.emit(id, model.Event{Type: model.EvOutputChunk, Data: map[string]string{
			"cid": cid, "stream": stream, "data": line,
		}})
	})
	a.emit(id, model.Event{Type: model.EvCommandDone, Data: map[string]any{"cid": cid, "exit_code": res.ExitCode}})

	out := res.Output
	limit := a.cfg.Agent.OutputLimit
	if len(out) > limit {
		out = out[:limit] + fmt.Sprintf("\n...[truncated, %d bytes total]", len(res.Output))
	}
	return fmt.Sprintf("exit_code=%d\n%s", res.ExitCode, out)
}

// runBrowserCapture opens a URL in headless Chrome and captures its network
// requests, surfacing the discovered endpoints in the terminal UI.
func (a *Agent) runBrowserCapture(ctx context.Context, id, target, cookie string, waitS int) string {
	if !a.cfg.Browser.Enabled {
		return "browser_capture is disabled. Enable it (browser.enabled: true) in Settings."
	}
	if cookie == "" { // fall back to the engagement's session cookie (authenticated discovery)
		if eng, err := a.st.Get(id); err == nil {
			cookie = eng.Cookie
		}
	}
	if fixed, changed := a.fixTarget(id, target); changed { // never capture a placeholder host
		target = fixed
	}
	cid := fmt.Sprintf("c%d", time.Now().UnixNano())
	a.emit(id, model.Event{Type: model.EvCommandStarted, Data: map[string]string{"cid": cid, "cmdline": "browser ▶ open " + target}})

	nav := time.Duration(a.cfg.Browser.NavWaitS) * time.Second
	if waitS > 0 {
		nav = time.Duration(waitS) * time.Second
	}
	res, err := browser.Capture(ctx, browser.Config{
		ChromePath: a.cfg.Browser.ChromePath,
		Headless:   a.cfg.Browser.Headless,
		NavWait:     nav,
		Timeout:     time.Duration(a.cfg.Browser.TimeoutS) * time.Second,
		RemoteURL:   a.cfg.Browser.RemoteURL,
		AutoLaunch:  a.cfg.Browser.AutoLaunch,
		UserDataDir: a.cfg.Browser.UserDataDir,
	}, target, cookie, nil)
	if err != nil {
		a.emit(id, model.Event{Type: model.EvOutputChunk, Data: map[string]string{"cid": cid, "stream": "stderr", "data": err.Error()}})
		a.emit(id, model.Event{Type: model.EvCommandDone, Data: map[string]any{"cid": cid, "exit_code": 1}})
		return "browser_capture error: " + err.Error()
	}

	// Persist discovered endpoints (for idor_scan) and links (for the UI), deduped.
	_ = a.st.Update(id, func(e *model.Engagement) {
		seen := map[string]bool{}
		for _, ep := range e.Endpoints {
			seen[ep] = true
		}
		for _, ep := range res.Endpoints {
			if !seen[ep] {
				e.Endpoints = append(e.Endpoints, ep)
				seen[ep] = true
			}
		}
		linkSeen := map[string]bool{}
		for _, l := range e.Links {
			linkSeen[l] = true
		}
		for _, l := range res.Links {
			if !linkSeen[l] && len(e.Links) < 3000 { // cap to bound storage
				e.Links = append(e.Links, l)
				linkSeen[l] = true
			}
		}
	})

	// Adopt the browser's live session cookies so terminal scanners (curl/ffuf)
	// and idor_scan run authenticated with the same login the operator has in Chrome.
	if res.Cookies != "" {
		_ = a.st.Update(id, func(e *model.Engagement) { e.Cookie = res.Cookies })
		a.emit(id, model.Event{Type: model.EvAgentMessage, Data: map[string]string{
			"text": fmt.Sprintf("🔑 captured %d session cookie(s) from the browser — curl/ffuf/idor_scan will reuse this login", strings.Count(res.Cookies, "=")),
		}})
	}

	a.emit(id, model.Event{Type: model.EvOutputChunk, Data: map[string]string{"cid": cid, "stream": "stdout",
		"data": fmt.Sprintf("captured %d requests · %d same-origin endpoints · %d links", res.Total, len(res.Endpoints), len(res.Links))}})
	for i, e := range res.Endpoints {
		if i >= 60 {
			break
		}
		a.emit(id, model.Event{Type: model.EvOutputChunk, Data: map[string]string{"cid": cid, "stream": "stdout", "data": e}})
	}
	a.emit(id, model.Event{Type: model.EvCommandDone, Data: map[string]any{"cid": cid, "exit_code": 0}})

	var sb strings.Builder
	fmt.Fprintf(&sb, "Loaded %s in headless Chrome. Captured %d network requests.\n\nSame-origin endpoints (METHOD URL):\n", target, res.Total)
	for i, e := range res.Endpoints {
		if i >= 120 {
			fmt.Fprintf(&sb, "...(%d more)\n", len(res.Endpoints)-i)
			break
		}
		sb.WriteString(e + "\n")
	}
	if len(res.Links) > 0 {
		sb.WriteString("\nSame-origin page links:\n")
		for i, l := range res.Links {
			if i >= 60 {
				fmt.Fprintf(&sb, "...(%d more)\n", len(res.Links)-i)
				break
			}
			sb.WriteString(l + "\n")
		}
	}
	out := sb.String()
	if len(out) > a.cfg.Agent.OutputLimit {
		out = out[:a.cfg.Agent.OutputLimit] + "\n...[truncated]"
	}
	return out
}

// awaitDecision pauses on an error and waits for the operator to choose
// retry or skip (reusing the reply channel). Returns the chosen option text.
func (a *Agent) awaitDecision(ctx context.Context, id, errMsg string) string {
	// Autonomous (squad) runs have no operator to block on — self-heal transient
	// gateway errors by backing off and retrying, bounded by the time budget.
	a.mu.Lock()
	auto := a.autonomous[id]
	a.mu.Unlock()
	if auto {
		a.emit(id, model.Event{Type: model.EvAgentMessage, Data: map[string]string{"text": "⏳ transient error, retrying: " + truncURL(errMsg)}})
		select {
		case <-ctx.Done():
			return "skip"
		case <-time.After(5 * time.Second):
			return "retry"
		}
	}

	_ = a.st.Update(id, func(e *model.Engagement) {
		e.Status = "awaiting_input"
		e.Question = &model.OperatorAsk{
			Prompt:    errMsg,
			Questions: []string{"A job failed. Retry it, or skip and finish with what you have?"},
			Options:   []string{"retry", "skip"},
		}
	})
	a.emit(id, model.Event{Type: model.EvAwaitingInput, Data: map[string]any{
		"prompt": errMsg, "questions": []string{"A job failed. Retry it, or skip and finish?"}, "options": []string{"retry", "skip"},
	}})

	a.mu.Lock()
	ch := a.replies[id]
	a.mu.Unlock()
	select {
	case <-ctx.Done():
		return "skip"
	case reply := <-ch:
		_ = a.st.Update(id, func(e *model.Engagement) {
			e.Status = "running"
			e.Question = nil
		})
		a.setStatus(id, "running")
		return reply
	}
}

// logLLM appends one request/response round-trip to data/<id>.llm.jsonl so every
// AI interaction is saved for auditing/debugging.
func (a *Agent) logLLM(id, system string, messages []llm.Message, resp *llm.Response, callErr error) {
	dir := a.cfg.Storage.Dir
	if dir == "" {
		return
	}
	entry := map[string]any{
		"ts":       time.Now().Format(time.RFC3339Nano),
		"request":  map[string]any{"system": system, "messages": messages},
		"response": resp,
	}
	if callErr != nil {
		entry["error"] = callErr.Error()
	}
	b, err := json.Marshal(entry)
	if err != nil {
		return
	}
	f, err := os.OpenFile(filepath.Join(dir, id+".llm.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	f.Write(append(b, '\n'))
}

// askOperator pauses the loop until the operator replies.
func (a *Agent) askOperator(ctx context.Context, id, prompt string, questions []string) string {
	// In squad (autonomous) mode there is no single operator to block on — scope
	// is pre-authorized, so answer automatically and keep working.
	a.mu.Lock()
	auto := a.autonomous[id]
	a.mu.Unlock()
	if auto {
		return "Scope is pre-authorized for this autonomous squad run — proceed without asking. Details were provided in your brief."
	}

	_ = a.st.Update(id, func(e *model.Engagement) {
		e.Status = "awaiting_input"
		e.Question = &model.OperatorAsk{Prompt: prompt, Questions: questions}
	})
	a.emit(id, model.Event{Type: model.EvAwaitingInput, Data: map[string]any{
		"prompt": prompt, "questions": questions,
	}})

	a.mu.Lock()
	ch := a.replies[id]
	a.mu.Unlock()

	select {
	case <-ctx.Done():
		return "operator did not reply (engagement stopped)"
	case reply := <-ch:
		_ = a.st.Update(id, func(e *model.Engagement) {
			e.Status = "running"
			e.Question = nil
		})
		a.setStatus(id, "running")
		return "operator answered:\n" + reply
	}
}

// ---- helpers ----

func (a *Agent) recordFinding(id string, f model.Finding) {
	f.ID = fmt.Sprintf("f%d", time.Now().UnixNano())
	_ = a.st.Update(id, func(e *model.Engagement) { e.Findings = append(e.Findings, f) })
	a.emit(id, model.Event{Type: model.EvFinding, StepID: f.StepID, Data: f})
}

func (a *Agent) emit(id string, ev model.Event) {
	ev.Timestamp = time.Now()
	_ = a.st.Update(id, func(e *model.Engagement) { e.Events = append(e.Events, ev) })
	if b, err := json.Marshal(ev); err == nil {
		a.hub.Publish(id, b)
	}
}

func (a *Agent) setStatus(id, status string) {
	_ = a.st.Update(id, func(e *model.Engagement) {
		e.Status = status
		if status == "done" || status == "error" || status == "stopped" {
			now := time.Now()
			e.FinishedAt = &now
		}
	})
	a.emit(id, model.Event{Type: model.EvStatus, Data: map[string]string{"status": status}})
}

func (a *Agent) setStepStatus(id, stepID, status string) {
	if stepID == "" {
		return
	}
	var phase string
	_ = a.st.Update(id, func(e *model.Engagement) {
		for i := range e.Roadmap {
			if e.Roadmap[i].ID == stepID {
				e.Roadmap[i].Status = status
				phase = e.Roadmap[i].Phase
			}
		}
		if phase != "" {
			e.Phase = phase
		}
	})
	if phase != "" {
		a.emit(id, model.Event{Type: model.EvPhaseChanged, Data: map[string]string{"phase": phase}})
	}
}
