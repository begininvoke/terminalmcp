package agent

import (
	"strings"

	"terminalmcp/internal/llm"
)

const systemPrompt = `You are an autonomous black-box penetration testing agent operating inside an
authorized, isolated sandbox. You have explicit permission to test the in-scope targets.
There are NO artificial restrictions on tools, commands, ports, or options — use whatever the
engagement requires.

EXECUTION MODEL
- Run ANY command-line tool via the "run_command" tool (curl, ffuf, gobuster, nuclei, sqlmap,
  nmap, whatweb, jq, etc.). Read its output, then decide the next command. There are no per-tool
  wrappers; the terminal is your single execution layer.
- Use "browser_capture" to open a URL in a REAL headless Chrome and capture the network requests
  it makes — this is how you discover JS-rendered routes and API endpoints that a plain HTTP
  crawl misses. Pass a "cookie" to capture as a logged-in user (required for IDOR). If the operator
  configured a session cookie for this engagement, browser_capture and idor_scan use it automatically.

INTAKE FIRST (required)
- Before running ANY test, analyze the target and call "ask_operator" to gather: authorization
  confirmation, in-scope hosts/domains/URLs, out-of-scope items, target type, credentials/test
  accounts (and a SECOND account if testing IDOR), and constraints (rate limits, fragile
  systems, test window, destructive tests yes/no). You MAY run passive read-only lookups
  (dig, whois, curl -I) first. Only after the operator answers, emit the roadmap and begin.

METHODOLOGY (follow this order; one roadmap step per phase)
1. DISCOVERY — enumerate the attack surface. Call "browser_capture" on the target to open it in
   Chrome and capture the network requests (the routes/API endpoints it actually calls). Also
   brute endpoints with ffuf/gobuster. Build a concrete list of URLs, parameters, and API
   endpoints (note any with an object id like /api/orders/12345, ?user_id=, /profile/{id}).
2. ACCESS-CONTROL / IDOR & COMMON BUGS — call "idor_scan" to test EVERY discovered endpoint at
   once (it defaults to all endpoints from browser_capture — do NOT hand-pick one). Pass
   cookie_a and cookie_b if you have two test accounts. Review its per-endpoint table, then
   CONFIRM the flagged ones with a clean curl reproduction and record each as a finding. Also
   probe authz bypass, injection, SSRF, open redirect, and info leak where relevant.
3. FUZZING — fuzz discovered parameters/endpoints (ffuf, payload lists) to surface anomalies.
4. BYPASS — for promising-but-blocked cases, apply bypass techniques (403/WAF bypass: header
   tricks, path/case/encoding mutations; auth bypass; method override) and retry.
5. FALSE-POSITIVE VERIFICATION — before reporting, re-test and confirm each candidate finding
   with a clean, minimal reproduction. Drop anything not reproducible. Mark confirmed vs.
   suspected in the evidence.
6. REPORT — call "finalize_report" with confirmed findings, severities, and remediation.

WORKING JOURNAL (think like a real pentester)
- Keep a Markdown journal with "add_note": after EVERY method, record what you tried and whether it WORKED or
  FAILED (and why), plus new IDEAS to try. Your journal is shown to you before each step.
- Before choosing the next action, REVIEW your journal: do not repeat a method that already failed; instead
  invent a NEW method (combine tools, change inputs/encodings, target a different surface) to reach the goal.
- You may use ANY installed tool and ANY technique. Keep going, varying your approach, until the goal is
  confirmed or the time budget runs out.

NARRATION (keep the operator's UI populated)
- "update_roadmap" once you know the plan; keep step statuses current.
- "begin_step" before each step. After running commands: "record_analysis" (with risk),
  "add_finding" per confirmed/suspected issue, and "recommend" next actions.
- Respect the rate limit if one is configured. Prefer non-destructive techniques first;
  escalate only within scope.`

const idorPlaybook = `IDOR / broken-access-control techniques — try EVERY one before concluding:
- browser_capture (authenticated, the engagement cookie is applied automatically) then idor_scan over ALL endpoints.
- For each id-bearing endpoint: replay with another account (cookie_a vs cookie_b); increment/decrement numeric ids; try id-1, id+1, 0, negative, very large; try another account's UUID.
- Move the id between locations: path segment, query param, JSON body, and headers (X-User-Id, etc.).
- Change method (GET<->POST), add/remove trailing slash, switch API version (v1/v2/v3).
- Try array/wrapped ids ([id], id[]=), parameter pollution (id=A&id=B), wildcard/empty values.
- Test nested/related objects (/orders/{id}/items, /users/{id}/addresses) and export/download/share endpoints.
- Test with no session and with a low-privilege session.
CONFIRMATION RULE (strict): an IDOR is "confirmed" ONLY when idor_scan, given TWO accounts (cookie_a AND
cookie_b), shows Account B receiving Account A's data while the unauthenticated response differs. You CANNOT
self-confirm IDOR — value tricks (id=0, id=-1, id±1) are weak candidates, never proof. Without two account
cookies, the goal cannot be confirmed; report only 'suspected' candidates and say two accounts are needed.`

// goalDirective focuses the engagement on a single objective and demands
// exhaustive, confirm-and-exploit behavior.
func goalDirective(goal string) string {
	s := "\n\nPRIMARY GOAL: " + goal + "\n" +
		"Concentrate the ENTIRE engagement on this goal. Be EXHAUSTIVE — systematically try every relevant technique " +
		"before you finalize; do NOT stop after one or two attempts. When you find a candidate, CONFIRM it with a clean " +
		"reproduction and attempt safe exploitation to prove real impact (record the finding with confidence=confirmed). " +
		"Only call finalize_report once the goal is confirmed or you have genuinely exhausted the techniques.\n"
	g := strings.ToLower(goal)
	if strings.Contains(g, "idor") || strings.Contains(g, "bola") || strings.Contains(g, "access") {
		s += idorPlaybook
	}
	return s
}

// methodStrategies are distinct angles the agent rotates through when its
// current approach isn't finding the bug — so it CHANGES method and retries
// instead of repeating itself.
var methodStrategies = []string{
	"Re-run browser_capture (authenticated, with the engagement cookie) and crawl DEEPER — open key links/sub-pages you haven't visited yet — to discover NEW endpoints and parameters, then idor_scan them.",
	"Switch vulnerability class. Fuzz parameters for reflected/stored XSS (use the xss wordlist) and for injection (sqli wordlist) with ffuf, and inspect responses for reflection or SQL errors.",
	"Attack inputs differently: vary encodings (URL/double-URL/unicode/base64), change HTTP method (GET<->POST), and move the payload between query string, JSON body, headers, and cookies; try parameter pollution (a=1&a=2).",
	"Run nuclei against the target for known CVEs/misconfigurations, and whatweb to fingerprint the stack; then target known issues for that technology.",
	"Inspect the JavaScript bundles and JSON responses for hidden/undocumented API endpoints, secrets/tokens, and debug parameters; then test any new endpoints you find.",
	"Test authorization differently: hit sensitive endpoints with NO session, with a low-privilege session, force-browse to admin/internal paths, and (for IDOR) compare two accounts with idor_scan.",
	"Brute more content with ffuf/gobuster using the common.txt wordlist plus extensions (.php,.bak,.json,.zip), and probe boundary/error cases (id=0,-1,huge, missing/extra params) for info leaks.",
	"Re-examine everything captured so far: pick the most sensitive endpoints (auth, user data, admin, payment, export) and hand-craft targeted exploit requests with curl, checking status/length/content differences.",
}

// nextStrategy returns a different strategy each attempt (round-robin).
func nextStrategy(attempt int) string {
	if attempt < 1 {
		attempt = 1
	}
	return methodStrategies[(attempt-1)%len(methodStrategies)]
}

// toolDefs returns the tool schemas exposed to the model.
func toolDefs() []llm.Tool {
	return []llm.Tool{
		{
			Name:        "run_command",
			Description: "Execute a shell command in the sandbox and return its combined stdout/stderr. Use this for ALL command-line tools.",
			InputSchema: rawSchema(`{
				"type":"object",
				"properties":{
					"command":{"type":"string","description":"The full shell command to run."},
					"timeout_s":{"type":"integer","description":"Optional timeout in seconds."}
				},
				"required":["command"]
			}`),
		},
		{
			Name: "browser_capture",
			Description: "Open a URL in a real (headless) Chrome, let it load, and capture every network request it makes — " +
				"the way to discover JS-rendered routes and API endpoints. Returns captured requests and same-origin endpoints. " +
				"Pass 'cookie' to capture while authenticated (needed for IDOR testing).",
			InputSchema: rawSchema(`{
				"type":"object",
				"properties":{
					"url":{"type":"string","description":"The page URL to open."},
					"cookie":{"type":"string","description":"Optional Cookie header value for an authenticated session."},
					"wait_s":{"type":"integer","description":"Optional seconds to wait for XHR/fetch to fire."}
				},
				"required":["url"]
			}`),
		},
		{
			Name: "idor_scan",
			Description: "Deterministically test EVERY discovered endpoint for IDOR / broken access control. " +
				"For each id-bearing GET endpoint it replays the request as Account A, as Account B, and with NO session, " +
				"then compares status codes and response sizes to flag cross-account or unauthenticated access. " +
				"Defaults to ALL endpoints found by browser_capture (do not cherry-pick). Only sends GET requests (non-destructive).",
			InputSchema: rawSchema(`{
				"type":"object",
				"properties":{
					"endpoints":{"type":"array","items":{"type":"string"},"description":"Optional explicit list (METHOD URL or URL). Omit to test ALL discovered endpoints."},
					"cookie_a":{"type":"string","description":"Cookie header for Account A (the session used during capture)."},
					"cookie_b":{"type":"string","description":"Cookie header for Account B (used to detect cross-account access)."},
					"max":{"type":"integer","description":"Max endpoints to test (default 60)."}
				}
			}`),
		},
		{
			Name:        "search_skills",
			Description: "Search the expert cybersecurity skill playbooks by keyword (e.g. 'idor', 'xss', 'jwt', 'ssrf', 'wordpress'). Returns matching skill names + descriptions. Then call load_skill to read the chosen playbook.",
			InputSchema: rawSchema(`{
				"type":"object",
				"properties":{
					"query":{"type":"string","description":"Keywords describing the technique/target you need a playbook for."},
					"limit":{"type":"integer","description":"Max results (default 12)."}
				},
				"required":["query"]
			}`),
		},
		{
			Name:        "load_skill",
			Description: "Load an expert skill playbook (its SKILL.md) by exact name, then follow its step-by-step method.",
			InputSchema: rawSchema(`{
				"type":"object",
				"properties":{"name":{"type":"string","description":"Exact skill name from search_skills."}},
				"required":["name"]
			}`),
		},
		{
			Name: "remember",
			Description: "Save a durable lesson to long-term memory (memory.md) that should help on FUTURE engagements too — " +
				"e.g. 'target uses Keycloak SSO on auth.x.com', 'API is on api.x.com', 'WAF blocks <script> but not <svg>'. This memory is shown at the start of every engagement.",
			InputSchema: rawSchema(`{
				"type":"object",
				"properties":{"text":{"type":"string"}},
				"required":["text"]
			}`),
		},
		{
			Name: "add_note",
			Description: "Append to your Markdown working journal: what method you tried, whether it WORKED or FAILED (with why), and your next IDEAS. " +
				"Your journal is shown to you before every step — review it, never repeat a failed method, and invent NEW methods to reach the goal.",
			InputSchema: rawSchema(`{
				"type":"object",
				"properties":{
					"status":{"type":"string","enum":["tried","worked","failed","idea"],"description":"What kind of note this is."},
					"text":{"type":"string","description":"The note, e.g. 'ffuf XSS on ?q= reflected nothing (WAF 403). Next: try header injection.'"}
				},
				"required":["text"]
			}`),
		},
		{
			Name:        "update_roadmap",
			Description: "Set or update the step-by-step testing roadmap shown in the UI.",
			InputSchema: rawSchema(`{
				"type":"object",
				"properties":{
					"steps":{"type":"array","items":{"type":"object","properties":{
						"id":{"type":"string"},
						"phase":{"type":"string"},
						"title":{"type":"string"},
						"intent":{"type":"string"},
						"status":{"type":"string","enum":["pending","running","done","skipped","failed"]}
					},"required":["id","phase","title","status"]}}
				},
				"required":["steps"]
			}`),
		},
		{
			Name:        "begin_step",
			Description: "Mark the start of a roadmap step and explain why you are doing it.",
			InputSchema: rawSchema(`{
				"type":"object",
				"properties":{
					"step_id":{"type":"string"},
					"title":{"type":"string"},
					"rationale":{"type":"string"}
				},
				"required":["step_id","title"]
			}`),
		},
		{
			Name:        "record_analysis",
			Description: "Record your analysis of the latest output with an overall risk level for the step.",
			InputSchema: rawSchema(`{
				"type":"object",
				"properties":{
					"step_id":{"type":"string"},
					"summary":{"type":"string"},
					"risk":{"type":"string","enum":["info","low","medium","high","critical"]}
				},
				"required":["summary","risk"]
			}`),
		},
		{
			Name: "add_finding",
			Description: "Record a security finding using the standard bug template. Fill as many fields as you can — " +
				"especially type, cwe, endpoint, parameter, impact, steps_to_reproduce, request/response, and remediation. " +
				"Set confidence to 'confirmed' only after a clean reproduction; otherwise 'suspected'.",
			InputSchema: rawSchema(`{
				"type":"object",
				"properties":{
					"step_id":{"type":"string"},
					"title":{"type":"string","description":"Short finding title."},
					"severity":{"type":"string","enum":["info","low","medium","high","critical"]},
					"confidence":{"type":"string","enum":["confirmed","suspected","false_positive"]},
					"type":{"type":"string","description":"Vulnerability class, e.g. IDOR, XSS, SQLi, SSRF, BrokenAuth."},
					"cwe":{"type":"string","description":"e.g. CWE-639"},
					"owasp":{"type":"string","description":"e.g. A01:2021-Broken Access Control"},
					"cvss":{"type":"string","description":"CVSS score and/or vector, e.g. 8.1 / CVSS:3.1/AV:N/..."},
					"target":{"type":"string","description":"Host/app under test."},
					"endpoint":{"type":"string","description":"Affected URL/route."},
					"parameter":{"type":"string","description":"Affected parameter, e.g. global_user_id."},
					"description":{"type":"string"},
					"impact":{"type":"string"},
					"steps_to_reproduce":{"type":"array","items":{"type":"string"}},
					"request":{"type":"string","description":"Raw HTTP request used (redact secrets)."},
					"response":{"type":"string","description":"Relevant response snippet as evidence."},
					"evidence":{"type":"string","description":"Why this proves the issue."},
					"remediation":{"type":"string"},
					"references":{"type":"array","items":{"type":"string"}}
				},
				"required":["title","severity","target","type"]
			}`),
		},
		{
			Name:        "recommend",
			Description: "Recommend the next actions after a step.",
			InputSchema: rawSchema(`{
				"type":"object",
				"properties":{
					"step_id":{"type":"string"},
					"next_actions":{"type":"array","items":{"type":"object","properties":{
						"title":{"type":"string"},
						"why":{"type":"string"}
					},"required":["title"]}}
				},
				"required":["next_actions"]
			}`),
		},
		{
			Name:        "ask_operator",
			Description: "Pause and ask the operator for required details (used for intake). The loop resumes with their answer.",
			InputSchema: rawSchema(`{
				"type":"object",
				"properties":{
					"prompt":{"type":"string","description":"Context for why you are asking."},
					"questions":{"type":"array","items":{"type":"string"}}
				},
				"required":["questions"]
			}`),
		},
		{
			Name:        "finalize_report",
			Description: "Produce the final report summary. Call this when testing is complete.",
			InputSchema: rawSchema(`{
				"type":"object",
				"properties":{
					"executive_summary":{"type":"string"},
					"methodology":{"type":"string"},
					"scope":{"type":"string"}
				},
				"required":["executive_summary"]
			}`),
		},
	}
}
