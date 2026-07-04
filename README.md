# AI Black-Box Pentest Platform

An autonomous, AI-driven black-box penetration testing app. A **Go backend** drives
an LLM agent that runs **any CLI tool through a single generic Terminal execution
layer** (no per-tool MCP wrappers), reads the output, analyzes it, and decides the
next step. A **React + shadcn-style frontend** lets the operator **enter/edit the
first prompt**, then shows the roadmap, live terminal, analysis, findings + risk,
recommendations, and the final report.

See [DESIGN.md](DESIGN.md) for the full architecture.

## Quick start

Two processes: the Go backend (`:8080`) and the Vite frontend (`:5173`).

```bash
# 1) Backend
cd backend
go run ./cmd/server            # reads ../config.yml; falls back to mock mode if no API key

# 2) Frontend (new terminal)
cd frontend
npm install
npm run dev                    # http://localhost:5173
```

Open http://localhost:5173, edit the first prompt, and click **Start engagement**.

### LLM providers

Configure under `llm:` in `config.yml`. Four providers:

| provider | use for | tool-calling |
|---|---|---|
| `anthropic` | Claude direct | native |
| `openrouter` | OpenRouter (any model id) | native |
| `openai` | any OpenAI-compatible endpoint (vLLM/LiteLLM gateways, Ollama, …) | `native` or `prompt` |
| `mock` | scripted demo, no key | n/a |

Export the key named by `api_key_env` (the key is never stored in the file):

```bash
export LLM_API_KEY=...           # for the configured openai/openrouter provider
# or: export ANTHROPIC_API_KEY=sk-ant-...   # for provider: anthropic
```

**`tool_mode` (openai/openrouter only).** The agent loop needs tool-calling.
- `native` — sends the API `tools[]` param (OpenRouter, and any server with
  function-calling enabled).
- `prompt` — the server has **no** tool-calling, so tools are described in the
  system prompt and the model replies with a single JSON action
  `{"tool":"...","input":{...}}` that the backend parses.

> The bundled Digikala gateway example (`openai/Qwen-Qwen3-4B-Instruct`) returns
> HTTP 400 on native tools, so it is configured with `tool_mode: prompt`. Verified
> working: the model drives intake and roadmap generation via JSON actions.

Without a key (or with `provider: mock`) the app runs a **scripted demo** that
exercises the entire UI — a real command through the terminal layer and the
intake pause — so you can try it with zero setup.

## Chrome MCP (browser-based testing)

For browser-shaped tasks (JS-rendered apps, login flows, stored-XSS, CSP) the
agent can drive a **Chrome MCP** server. Configured under `mcp.chrome` in
`config.yml`.

A Chrome MCP exposes itself one of two ways — its "address" is one of these:

1. **stdio** — you launch a command and talk over stdin/stdout. Example:
   [Browser MCP](https://browsermcp.io) (`npx @browsermcp/mcp@latest`) bridges to
   a Chrome **extension** over a local WebSocket. Set:
   ```yaml
   mcp:
     chrome:
       enabled: true
       transport: stdio
       command: ["npx", "-y", "@browsermcp/mcp@latest"]
   ```
2. **HTTP/SSE** — the server listens on a URL. Set:
   ```yaml
   mcp:
     chrome:
       enabled: true
       transport: sse
       url: http://localhost:12306/sse
   ```

### How to check the connection

- **Browser MCP (stdio + extension):** install the extension, open a tab, click
  the extension and press **Connect**, and make sure `npx @browsermcp/mcp@latest`
  is running. A working connection answers `list_tabs`/`get_current_tab`; if you
  get *"Chrome is not running / not connected"* the extension isn't bridged to a
  live tab — reopen Chrome and re-click Connect on the target tab.
- **HTTP/SSE Chrome MCP:** verify the endpoint responds and lists tools:
  ```bash
  curl -N http://localhost:12306/sse                 # should hold open an SSE stream
  # MCP handshake (initialize → tools/list) over the server's message endpoint
  ```
- **Quick smoke test (any Chrome MCP):** ask it to `navigate`/`open_url` to
  `http://localhost:5173` and read the page title back — if the title is
  "AI Black-Box Pentest" the round-trip works.

## How it works

1. The operator's first prompt creates an engagement (`POST /api/engagements`).
2. The agent loop (Claude tool-use) runs:
   - `run_command` — the single execution layer for ALL CLI tools
   - `update_roadmap`, `begin_step`, `record_analysis`, `add_finding`,
     `recommend`, `ask_operator`, `finalize_report` — UI/state narration
3. Events stream to the browser over WebSocket (`/ws?engagement=<id>`).
4. Intake first: the agent pauses with `ask_operator` to confirm scope before testing.

## Status

| Piece | State |
|---|---|
| Go backend, config.yml, REST + WebSocket | ✅ built |
| Agent loop (tool-use) + mock mode | ✅ built |
| LLM providers: Anthropic, OpenRouter, OpenAI-compatible (native + prompt tool modes) | ✅ built + verified |
| Single generic Terminal execution layer + audit log | ✅ built (in-process) |
| React + shadcn-style UI (first prompt, roadmap, terminal, analysis, findings, report) | ✅ built |
| Settings panel — view/edit config live (provider, model, rate limit, Chrome MCP) + persist | ✅ built + verified |
| Rate limiting (commands/min, enforced before each command) | ✅ built |
| Logs panel — live event stream + command audit log | ✅ built + verified |
| Methodology baked into the agent (discover → IDOR → fuzz → bypass → verify FP → report) | ✅ built |
| Progress bar + per-step status (roadmap completion %) | ✅ built + verified |
| Engagement persistence (JSON on disk, survives restart) + History view | ✅ built + verified |
| Deep-link engagements by URL (`#e=<id>`), reopen full details via event replay | ✅ built + verified |
| `browser_capture` — headless-Chrome tool: load URL, capture network requests, extract endpoints (subdomains incl., assets filtered) for discovery + IDOR | ✅ built + verified |
| Honest terminal status (`completed` vs `incomplete`) + self-nudge to finish the roadmap | ✅ built |
| Structured finding JSON template (type/cwe/owasp/cvss/endpoint/steps/req-resp/remediation) + per-finding Copy JSON & export-all | ✅ built + verified |
| Chrome MCP config + connection/check guidance | ✅ documented |
| Externalize Terminal layer as a standalone MCP server (stdio/HTTP) / MCPO mode | ⬜ designed, not wired |
| SQLite persistence (currently in-memory) | ⬜ designed, not wired |
