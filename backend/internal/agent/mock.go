package agent

import (
	"context"
	"encoding/json"

	"terminalmcp/internal/model"
)

// runMock drives a scripted engagement so the whole UI works without an API key.
// It exercises the same tool paths the real LLM loop uses, including a real
// command via the terminal executor and an intake pause.
func (a *Agent) runMock(ctx context.Context, id string) error {
	emit := func(name, input string) string {
		return a.execTool(ctx, id, name, json.RawMessage(input))
	}

	a.emit(id, model.Event{Type: model.EvAgentMessage, Data: map[string]string{
		"text": "[mock mode] Analyzing target and preparing the roadmap. Set llm.provider to anthropic in config.yml for a real engagement.",
	}})

	emit("update_roadmap", `{"steps":[
		{"id":"s1","phase":"intake","title":"Scope & rules of engagement","intent":"Confirm authorization and gather details","status":"pending"},
		{"id":"s2","phase":"recon","title":"Passive reconnaissance","intent":"Characterize the target","status":"pending"},
		{"id":"s3","phase":"scanning","title":"Service/port discovery","intent":"Identify exposed services","status":"pending"},
		{"id":"s4","phase":"reporting","title":"Report","intent":"Summarize findings","status":"pending"}
	]}`)

	// Intake — blocks until the operator answers.
	emit("begin_step", `{"step_id":"s1","title":"Scope & rules of engagement","rationale":"Confirm authorization and gather details."}`)
	reply := emit("ask_operator", `{"prompt":"Before testing I need to confirm scope.","questions":[
		"What is the in-scope target (host/domain/URL)?",
		"Are destructive tests permitted?",
		"What is the allowed test window?"
	]}`)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	_ = reply
	emit("record_analysis", `{"step_id":"s1","summary":"Scope confirmed with the operator.","risk":"info"}`)

	// Recon step with a real command.
	emit("begin_step", `{"step_id":"s2","title":"Passive reconnaissance","rationale":"Characterize the host environment before active scanning."}`)
	emit("run_command", `{"command":"uname -a && echo '---' && whoami"}`)
	emit("record_analysis", `{"step_id":"s2","summary":"Established the local execution environment. No external service contacted yet.","risk":"info"}`)

	// A demo finding + recommendation.
	emit("add_finding", `{"step_id":"s2","title":"Demonstration finding (mock mode)","severity":"low","target":"localhost","evidence":"This is a scripted finding produced in mock mode.","remediation":"Switch to anthropic provider to run real tests.","refs":[]}`)
	emit("recommend", `{"step_id":"s2","next_actions":[
		{"title":"Run an nmap service scan","why":"Identify open ports and versions on the in-scope host."},
		{"title":"Fingerprint web stack","why":"Use whatweb/curl to identify the application."}
	]}`)

	// Mark the scanning step as skipped in this demo, so the roadmap is consistent.
	emit("update_roadmap", `{"steps":[
		{"id":"s1","phase":"intake","title":"Scope & rules of engagement","intent":"Confirm authorization and gather details","status":"done"},
		{"id":"s2","phase":"recon","title":"Passive reconnaissance","intent":"Characterize the target","status":"done"},
		{"id":"s3","phase":"scanning","title":"Service/port discovery","intent":"Identify exposed services","status":"skipped"},
		{"id":"s4","phase":"reporting","title":"Report","intent":"Summarize findings","status":"running"}
	]}`)

	// Report.
	emit("begin_step", `{"step_id":"s4","title":"Report","rationale":"Summarize the engagement."}`)
	emit("finalize_report", `{"executive_summary":"Mock engagement complete (scripted demo). Set the LLM API key to run a real black-box test.","methodology":"recon -> scanning -> reporting (scripted)","scope":"localhost (demo)"}`)
	emit("record_analysis", `{"step_id":"s4","summary":"Engagement complete.","risk":"info"}`)
	return nil
}
