# AI-Powered Black-Box Penetration Testing Platform — Design

> An autonomous, AI-driven black-box pentest application. A Go backend drives an
> LLM agent that performs reconnaissance, scanning, enumeration, exploitation,
> and reporting by running **any CLI tool through a single Terminal MCP** (plus
> Chrome MCP for browser-based testing). A React + shadcn/ui frontend shows the
> roadmap, live command execution, output, analysis, findings, and the final
> report.

---

## 1. Goals & Non-Goals

### Goals
- **One generic execution layer.** The agent runs arbitrary shell commands via a
  single **Terminal MCP** server. No per-tool MCP wrappers (no `nmap-mcp`,
  `curl-mcp`, etc.). Any CLI tool installed in the sandbox is usable immediately.
- **Browser testing when needed.** The agent can switch to **Chrome MCP** for
  DOM/auth/XSS/CSP and other browser-driven checks.
- **Agentic loop.** Run a command → read output → analyze → decide next step,
  iterating until a phase goal is met. Driven by an LLM (Claude).
- **Intake first.** The very first interaction analyzes the target and asks for
  any missing details before any test runs.
- **Full transparency in the UI.** Plan/roadmap, current step, the exact command,
  raw output, the agent's analysis, findings + risk level, recommended next
  actions, and a final report.
- **No artificial constraints** on tools, targets, commands, or options — the
  whole thing runs inside an authorized, sandboxed environment.

### Non-Goals
- Not a replacement for human sign-off on rules of engagement.
- Not a SaaS multi-tenant product (single-operator / lab use assumed; can be
  hardened later).
- The "no limits" stance is about *capability*, not about *bypassing
  authorization* — see §11.

---

## 2. High-Level Architecture

```
┌──────────────────────────────────────────────────────────────────────────┐
│                         Frontend (React + shadcn/ui)                       │
│  Engagement intake · Roadmap · Live step view · Terminal stream · Findings │
│  Risk dashboard · Final report                                             │
└───────────────▲───────────────────────────────────┬──────────────────────┘
                │  REST (control)        WebSocket (live events / streaming) │
                │                                     │
┌───────────────┴─────────────────────────────────────▼─────────────────────┐
│                          Backend (Go) — config.yml                         │
│                                                                            │
│  ┌──────────────┐   ┌──────────────────┐   ┌───────────────────────────┐  │
│  │ HTTP/WS API  │──▶│ Engagement Mgr   │──▶│  Agent Orchestrator       │  │
│  │ (chi/echo)   │   │ (state machine)  │   │  (ReAct loop, LLM client) │  │
│  └──────────────┘   └──────────────────┘   └─────────────┬─────────────┘  │
│         │                    │                            │                 │
│  ┌──────▼──────┐    ┌────────▼─────────┐    ┌─────────────▼─────────────┐  │
│  │ Persistence │    │ Event Bus / Hub  │    │  Tool Router (MCP client)  │  │
│  │ (SQLite)    │    │ (pub/sub → WS)   │    │  terminal · chrome · ui    │  │
│  └─────────────┘    └──────────────────┘    └─────────────┬─────────────┘  │
└─────────────────────────────────────────────────────────┼─────────────────┘
                                                            │ MCP (stdio/HTTP)
              ┌─────────────────────────────────────────────┼──────────────┐
              ▼                                              ▼                │
   ┌─────────────────────┐                      ┌──────────────────────┐    │
   │  Terminal MCP        │                      │  Chrome MCP          │    │
   │  run_command,        │                      │  navigate, click,    │    │
   │  read_output, kill,  │                      │  eval, read_page,    │    │
   │  read_file, ...      │                      │  network, console    │    │
   └──────────┬───────────┘                      └──────────┬───────────┘    │
              │ exec in sandbox                              │ controls       │
              ▼                                              ▼                │
   ┌──────────────────────────────────────────────────────────────────┐    │
   │   Sandbox (container/VM): nmap, curl, nikto, ffuf, gobuster,       │    │
   │   sqlmap, nuclei, whatweb, dig, openssl, ... + headless Chrome     │    │
   └──────────────────────────────────────────────────────────────────┘    │
                                                                            ─┘
```

**Optional MCPO bridge.** If you prefer the backend to talk to MCP servers over
plain HTTP/OpenAPI instead of speaking MCP directly, put **MCPO** (MCP-to-OpenAPI
proxy) in front of the Terminal/Chrome MCP servers. The Go Tool Router then calls
REST endpoints. Both modes are supported (see §6.4); native MCP is the default
because it keeps streaming and tool schemas first-class.

---

## 3. Core Principle — One Execution Layer

The single most important design decision: **the agent's "hands" are one generic
Terminal MCP, not a catalog of tool-specific MCPs.**

```jsonc
// Terminal MCP tool surface (the ONLY execution tools the agent gets)
run_command   { command: string, cwd?: string, timeout_s?: number, stdin?: string, async?: bool }
read_output   { session_id: string, since?: cursor }     // poll long-running jobs
write_input   { session_id: string, data: string }        // interactive tools
kill          { session_id: string }
list_sessions { }
read_file     { path: string, max_bytes?: number }        // inspect artifacts/wordlists
write_file    { path: string, content: string }           // build payloads/configs
glob          { pattern: string }                          // discover tools/outputs
```

Why this matters:
- **Zero integration cost per tool.** Install `nuclei` in the sandbox image and
  the agent can use it the same minute — no code, no new MCP.
- **The LLM already knows the CLIs.** Claude knows nmap/ffuf/sqlmap flags. Let it
  compose commands directly instead of forcing them through narrow wrappers.
- **Composability.** Pipes, redirects, `jq`, `grep`, chaining, writing a quick
  one-off script — all available because it's a real shell.

Chrome MCP is the **second** execution layer, used only when a task is inherently
browser-shaped (JS-rendered apps, login flows, stored-XSS verification, CSP,
cookies/storage inspection).

A small third set of tools is **internal** (served by the Go backend itself, not
the sandbox) so the agent can drive the UI/state — see §5.3.

---

## 4. Component Breakdown

### 4.1 Backend (Go)
- HTTP + WebSocket server (`chi` or `echo` for routing; `nhooyr.io/websocket` or
  `gorilla/websocket`).
- Loads `config.yml` (`spf13/viper` or `gopkg.in/yaml.v3`).
- Owns engagement lifecycle, the agent loop, persistence, and the event hub.
- LLM client for Claude (Anthropic Go SDK / HTTP). Tool-use loop with streaming.

### 4.2 Agent Orchestrator
- Implements the **ReAct loop** (§5). Holds conversation state, dispatches tool
  calls to the Tool Router, feeds observations back to the model.
- Enforces a **phase state machine** (intake → recon → scanning → enumeration →
  exploitation → post-exploitation → reporting).
- Emits structured events (step begun, command run, output chunk, analysis,
  finding, recommendation, report) onto the Event Bus.

### 4.3 Tool Router (MCP client)
- One MCP client per configured server (`terminal`, `chrome`), plus the internal
  `ui`/`report` tools handled in-process.
- Normalizes tool schemas into the LLM tool list, routes tool calls by namespace,
  and streams long-running command output back as observations.

### 4.4 Terminal MCP server
- A standalone MCP server (Go or Node) that executes commands in the sandbox.
- Supports synchronous and **async/streaming** execution with session IDs so the
  agent can launch a long scan, keep working, and poll output.
- Mounts a working dir for artifacts (`/engagements/<id>/`).

### 4.5 Chrome MCP server
- Headless/headful Chromium controlled via MCP: navigate, click, fill, eval JS,
  read DOM/text, capture network + console, screenshot.

### 4.6 Event Bus / Hub
- In-process pub/sub fanning structured events to all WebSocket subscribers of an
  engagement. Also persists each event (append-only) for replay/report.

### 4.7 Persistence
- **SQLite** (default; `modernc.org/sqlite` for cgo-free) — engagements, steps,
  events, findings, artifacts metadata, final reports. Pluggable to Postgres.
- Raw artifacts (scan outputs, screenshots) on disk under the engagement dir;
  DB stores paths + hashes.

### 4.8 Frontend (React + shadcn/ui)
- Vite + React + TypeScript, shadcn/ui components, Tailwind, TanStack Query for
  REST, native WebSocket for the live stream, `xterm.js` for terminal rendering.

---

## 5. The Agent Loop

### 5.1 ReAct cycle
```
            ┌──────────────────────────────────────────────┐
            │  System prompt + engagement context + phase   │
            └───────────────────────┬───────────────────────┘
                                     ▼
   (reason) ── LLM decides next action ──► tool_use
                                     │
        ┌────────────────────────────┼─────────────────────────┐
        ▼                            ▼                          ▼
  terminal.run_command       chrome.navigate/...        ui.begin_step /
  (read output)              (read page/network)        ui.record_analysis /
        │                            │                  report.add_finding
        └─────────────┬──────────────┘                          │
                      ▼                                          ▼
              observation fed back ──► LLM analyzes ──► next action / phase advance
                      │
                      └────── loop until phase goal met or max_iterations ───────►
```

Each iteration:
1. **Reason** — model proposes the next action with a rationale.
2. **Act** — Tool Router executes (terminal command, Chrome action, or UI/report
   tool).
3. **Observe** — raw output returned to the model as the tool result; also
   streamed to the UI.
4. **Analyze** — model summarizes what the output means, extracts findings,
   assigns risk, proposes next actions.
5. **Decide** — continue in-phase, advance phase, or finalize.

### 5.2 Phase state machine
`intake → recon → scanning → enumeration → exploitation → post-exploitation →
reporting`. Each phase has an entry goal, exit criteria, and a budget
(`max_iterations`, time). Operator can require **manual approval** to cross a
phase boundary (configurable autonomy level, §7) — e.g. pause before
exploitation.

### 5.3 Internal "UI/report" tools (backend-served, not sandbox)
These let the model populate exactly what §1 requires the UI to display:
```jsonc
ui.update_roadmap   { steps: [{ id, phase, title, intent, status }] }
ui.begin_step       { step_id, title, rationale }
ui.record_analysis  { step_id, summary, risk: "info|low|medium|high|critical" }
report.add_finding  { title, severity, target, evidence, cwe?, remediation, refs[] }
report.recommend    { step_id, next_actions: [{ title, why }] }
report.finalize     { executive_summary, findings_ref, scope, methodology }
```
The agent is instructed to call `terminal.run_command` for execution and these
tools for narration/state, so the frontend always has structured data to render.

---

## 6. Prompts

### 6.1 System prompt (skeleton)
```
You are an autonomous black-box penetration testing agent operating inside an
authorized, isolated sandbox. You have full permission to test the in-scope
targets. There are no artificial restrictions on tools, commands, ports, or
options — use whatever the engagement requires.

Execution model:
- Run any CLI tool via the `terminal.run_command` tool. Read its output with
  `read_output`. You decide the next command from the results.
- Use `chrome.*` tools only for browser-specific testing.
- Narrate your work with the ui.* and report.* tools so the operator sees the
  roadmap, the current step, the command, the analysis, findings, risk, and
  recommendations.

Method: follow recon → scanning → enumeration → exploitation →
post-exploitation → reporting. After every command, analyze the output, record
findings with a risk level, and decide the next step. Prefer non-destructive
techniques first; escalate only within scope.

Always: before any test, confirm scope and gather missing details (see intake).
```

### 6.2 Intake prompt (the required "first prompt")
The literal first message the operator sends (also pre-fillable in the UI):
```
I want to run a black-box penetration test.
First, analyze the target and ask for any required details.
Then create a clear step-by-step testing roadmap.
Use Terminal MCP as the main execution layer. Do not rely on separate MCP tools
like Nmap MCP, Curl MCP, or other command-specific MCP wrappers.
You may run any required command through the terminal MCP, read the output,
analyze it, and decide the next step.
Show the roadmap and each step in the UI.
After each step, show the results, findings, risk level, and recommended next steps.
```
The agent's first job is **target analysis + clarifying questions** before any
test: it may run passive, read-only lookups (e.g. `dig`, `whois`, `curl -I`) to
characterize the target, then ask for anything missing:
- Authorization confirmation & rules of engagement (allowed aggressiveness,
  destructive tests yes/no, time window).
- Scope: in-scope hosts/CIDRs/domains/URLs, explicit out-of-scope items.
- Target type: web app / API / network / host / cloud.
- Credentials / test accounts (if grey-box assist is allowed).
- Constraints: rate limits, fragile systems, data handling rules.

Only after intake is satisfied does it emit the roadmap (`ui.update_roadmap`) and
start phase `recon`.

### 6.3 Structured outputs
Findings, analyses, and the roadmap are produced via the typed `ui.*`/`report.*`
tools (JSON Schema enforced), so the frontend renders them reliably rather than
parsing free text.

---

## 7. config.yml

```yaml
server:
  host: 0.0.0.0
  http_port: 8080
  ws_path: /ws

llm:
  provider: anthropic
  model: claude-opus-4-8          # or claude-sonnet-4-6 for cheaper loops
  api_key_env: ANTHROPIC_API_KEY  # read from env, not stored in file
  base_url: ""                    # optional override / gateway
  max_tokens: 8192
  temperature: 0.2
  max_tool_iterations: 200        # hard stop for the agent loop

agent:
  autonomy: semi                  # auto | semi | manual
  pause_before_phases: [exploitation, post-exploitation]
  command_timeout_s: 600          # default per-command; agent may override
  parallel_commands: 4
  phases: [recon, scanning, enumeration, exploitation, post-exploitation, reporting]

mcp:
  mode: native                    # native (stdio/http MCP) | mcpo (REST via MCPO)
  terminal:
    transport: stdio              # stdio | http
    command: ["terminal-mcp", "--workdir", "/engagements"]
    # http_url: http://terminal-mcp:7000   # if transport: http
  chrome:
    enabled: true
    transport: http
    http_url: http://chrome-mcp:9222
  mcpo:
    base_url: http://mcpo:8000     # used only when mode: mcpo
    api_key_env: MCPO_API_KEY

sandbox:
  workdir: /engagements
  # No tool/port/target allowlists — execution is unrestricted by design.
  # Authorization is enforced at the engagement scope level (see §11).

storage:
  driver: sqlite                  # sqlite | postgres
  dsn: file:/data/pentest.db
  artifacts_dir: /engagements

logging:
  level: info
  audit_log: /data/audit.log      # append-only record of every command run

auth:
  enabled: true
  mode: token                     # token | none(lab only)
  token_env: APP_AUTH_TOKEN
```

Secrets come from env vars referenced by `*_env` keys, never hard-coded in the
file.

---

## 8. Data Model

```
Engagement   id, name, scope(json), rules_of_engagement, status, phase,
             created_at, finished_at
RoadmapStep  id, engagement_id, phase, order, title, intent, status
             (pending|running|done|skipped|failed)
Event        id, engagement_id, step_id?, type, payload(json), ts   # append-only
Command      id, engagement_id, step_id, cmdline, cwd, exit_code,
             started_at, ended_at, stdout_ref, stderr_ref, session_id
Finding      id, engagement_id, step_id?, title, severity, target,
             evidence(json), cwe?, remediation, refs(json), status
Artifact     id, engagement_id, kind, path, sha256, bytes
Report       id, engagement_id, executive_summary, methodology(json),
             generated_at, export_path
```

`Event.type` ∈ `phase_changed | roadmap_updated | step_begin | command_started |
output_chunk | command_finished | analysis | finding | recommendation |
report_finalized | error | awaiting_approval`.

---

## 9. API & WebSocket Contracts

### REST (control plane)
```
POST /api/engagements            { name, scope, rules } → engagement
GET  /api/engagements/:id        → engagement + roadmap + findings
POST /api/engagements/:id/message{ text } → send operator reply (intake answers)
POST /api/engagements/:id/approve{ phase } → approve a gated phase boundary
POST /api/engagements/:id/pause | /resume | /stop
GET  /api/engagements/:id/report?format=md|pdf|json
GET  /api/engagements/:id/commands/:cid/output → raw stdout/stderr
```

### WebSocket (live plane) — `/ws?engagement=:id`
Server → client messages mirror `Event.type`. Example sequence:
```
{ "type":"roadmap_updated", "steps":[...] }
{ "type":"step_begin", "step_id":"s3", "title":"Service enumeration", "rationale":"..." }
{ "type":"command_started", "cid":"c12", "cmdline":"nmap -sV -p- 10.0.0.5" }
{ "type":"output_chunk", "cid":"c12", "stream":"stdout", "data":"..." }
{ "type":"command_finished", "cid":"c12", "exit_code":0 }
{ "type":"analysis", "step_id":"s3", "summary":"...", "risk":"medium" }
{ "type":"finding", "title":"Outdated OpenSSH 7.2", "severity":"medium", ... }
{ "type":"recommendation", "step_id":"s3", "next_actions":[...] }
```
Client → server: `operator_message`, `approve_phase`, `pause`, `resume`, `stop`.

---

## 10. Frontend (React + shadcn/ui)

Layout: left **Roadmap rail**, center **Active step / live terminal**, right
**Findings & risk**, top **engagement header** (target, phase, status, controls).

| View / component | shadcn primitives | Shows |
|---|---|---|
| **Intake panel** | `Form`, `Input`, `Textarea`, `Dialog` | Agent's clarifying questions + operator answers; scope/RoE entry |
| **Roadmap rail** | `Accordion`, `Badge`, `Progress` | Phases & steps with status; click to focus a step |
| **Step header** | `Card`, `Badge` | Current step title + rationale (the "why") |
| **Terminal view** | `xterm.js` in a `Card`, `ScrollArea` | The exact command + streamed stdout/stderr (live) |
| **Analysis panel** | `Card`, `Alert` | Agent's analysis of the output |
| **Findings table** | `Table`, `Badge`, `Sheet` | Title, severity, target, evidence, remediation |
| **Risk dashboard** | `Card`, charts | Counts by severity, overall risk gauge |
| **Recommendations** | `Card`, `Button` | Next actions; approve/skip when phase is gated |
| **Report view** | `Tabs`, `Separator` | Executive summary, methodology, findings, export (MD/PDF/JSON) |
| **Controls** | `Button`, `AlertDialog` | Pause / resume / stop / approve-phase |

State: TanStack Query for REST snapshots; a WS hook reduces live events into the
store so the terminal streams in real time and findings/roadmap update as the
agent works.

This component set maps 1:1 to your required display list: **plan/roadmap,
current step, command, output, analysis, findings + risk, recommended next
actions, final report**.

---

## 11. Sandbox, Authorization & Audit

"No artificial limits on tools/targets/commands" is honored **inside** the
sandbox — but the system still needs authorization and accountability so it's a
real pentest tool, not a blind weapon:

- **Isolation.** Run the sandbox as a dedicated container/VM/network namespace
  with egress limited to declared scope at the *infrastructure* level (firewall),
  not by crippling the agent's command set.
- **Scope of record.** Each engagement records explicit in-scope and out-of-scope
  targets and a signed-off rules-of-engagement blob; intake will not proceed to
  testing without it. (This is authorization, not capability throttling.)
- **Audit log.** Every `run_command` (cmdline, time, exit code, output hash) is
  appended to `audit_log` — full reproducibility and a chain of evidence.
- **Phase gating.** Optional manual approval before exploitation /
  post-exploitation (config `agent.pause_before_phases`).
- **Kill switch.** `stop` immediately terminates running sessions via
  `terminal.kill` and halts the loop.

---

## 12. Tech Stack

| Layer | Choice |
|---|---|
| Backend | Go 1.22+, `chi`/`echo`, `gorilla`/`nhooyr` websocket, `viper`/`yaml.v3` |
| LLM | Anthropic Claude (`claude-opus-4-8` / `claude-sonnet-4-6`), tool-use + streaming |
| MCP client | `mark3labs/mcp-go` (or official Go MCP SDK) |
| Terminal MCP | standalone MCP server (Go or Node) with async/session exec |
| Chrome MCP | headless Chromium MCP server |
| MCPO (optional) | MCP→OpenAPI proxy for REST access |
| Persistence | SQLite (`modernc.org/sqlite`) → Postgres optional |
| Frontend | Vite + React + TS, shadcn/ui, Tailwind, TanStack Query, xterm.js |
| Packaging | Docker Compose: `backend`, `terminal-mcp`, `chrome-mcp`, `frontend`, (`mcpo`) |

---

## 13. Suggested Repo Structure

```
terminalmcp/
├── config.yml
├── docker-compose.yml
├── backend/
│   ├── cmd/server/main.go
│   ├── internal/
│   │   ├── config/          # config.yml loading
│   │   ├── api/             # REST + WS handlers
│   │   ├── agent/           # ReAct loop, phase machine, prompts
│   │   ├── llm/             # Claude client (tool-use, streaming)
│   │   ├── mcp/             # Tool Router: terminal/chrome clients, MCPO mode
│   │   ├── tools/           # internal ui.*/report.* tool implementations
│   │   ├── engagement/      # lifecycle, state
│   │   ├── events/          # pub/sub hub
│   │   └── store/           # sqlite models + migrations
│   └── prompts/             # system.md, intake.md
├── terminal-mcp/            # the single generic execution MCP server
├── chrome-mcp/              # browser MCP (or reference an existing one)
└── frontend/
    ├── src/
    │   ├── components/      # shadcn-based views (§10)
    │   ├── hooks/           # useEngagement, useWebSocket
    │   ├── store/
    │   └── pages/
    └── ...
```

---

## 14. Implementation Roadmap (milestones)

1. **M1 — Skeleton.** Go server + config.yml loading; SQLite migrations; REST
   create/get engagement; WS echo.
2. **M2 — Terminal MCP + Router.** Generic `run_command`/`read_output` MCP;
   Go MCP client; run a hard-coded command end-to-end and stream output to a WS
   client.
3. **M3 — Agent loop.** Claude tool-use loop with `terminal.*` + `ui.*`/`report.*`
   tools; phase state machine; intake handling.
4. **M4 — Frontend.** shadcn views: intake, roadmap, terminal stream, analysis,
   findings, recommendations; WS wiring.
5. **M5 — Chrome MCP.** Browser tools wired into the router; agent uses them for
   web-app tasks.
6. **M6 — Reporting.** `report.finalize`, export MD/PDF/JSON, risk dashboard.
7. **M7 — Hardening.** Audit log, phase gating/approvals, pause/stop kill switch,
   MCPO mode, auth token.

---

## 15. End-to-End Example (abridged)

```
operator → (intake prompt)
agent    → curl -I https://target  ; dig target ; whois target   [passive recon]
agent    → ui.update_roadmap([...])  +  asks: "Confirm scope includes api.target?
            Destructive tests allowed? Test window?"
operator → answers
agent    → phase recon:   whatweb / nmap -sV  → analysis → finding(s) + risk
agent    → phase scanning: nuclei / nikto     → analysis → findings
agent    → phase enumeration: ffuf wordlist   → analysis → findings
agent    → (gated) awaiting_approval: exploitation
operator → approve
agent    → sqlmap / targeted PoC              → analysis → finding (critical)
agent    → chrome.navigate + eval             → verify stored XSS
agent    → report.finalize → executive summary + findings + remediation
```

Every arrow above produces WebSocket events that render in the UI as roadmap
updates, a live terminal, analysis cards, findings rows, and recommendations —
exactly the display surface specified.
```
