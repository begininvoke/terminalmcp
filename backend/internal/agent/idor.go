package agent

import (
	"context"
	"fmt"
	"io"
	"math"
	"net/http"
	"regexp"
	"strings"
	"time"

	"terminalmcp/internal/model"
)

// idRe detects endpoints that carry an object/user identifier — the ones worth
// IDOR testing (UUIDs, long numeric ids, or id-like query params).
var idRe = regexp.MustCompile(`(?i)([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}|/\d{3,}(/|$|\?)|(global_user_id|user_id|userid|customer_id|account|order_id|profile|uid|[?&]id)=)`)

type probeResult struct {
	status int
	length int
	err    error
}

// httpProbe issues one GET and returns status + body length (no redirect follow).
func httpProbe(ctx context.Context, rawURL, cookie string) probeResult {
	client := &http.Client{
		Timeout:       12 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return probeResult{err: err}
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (pentest-agent idor_scan)")
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	resp, err := client.Do(req)
	if err != nil {
		return probeResult{err: err}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	return probeResult{status: resp.StatusCode, length: len(body)}
}

func similar(a, b int) bool {
	if a <= 0 || b <= 0 {
		return false
	}
	return math.Abs(float64(a-b))/float64(a) <= 0.2
}

// parseEndpoint splits "GET https://..." (or a bare URL) into method + url.
func parseEndpoint(s string) (method, url string) {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, ' '); i > 0 && i <= 7 {
		return strings.ToUpper(s[:i]), strings.TrimSpace(s[i+1:])
	}
	return "GET", s
}

// runIdorScan deterministically tests every discovered endpoint for IDOR /
// broken access control — replaying each id-bearing GET as A, as B, and
// unauthenticated, then comparing responses. This guarantees full coverage
// instead of relying on the model to loop.
func (a *Agent) runIdorScan(ctx context.Context, id string, endpoints []string, cookieA, cookieB string, max int) string {
	if eng, err := a.st.Get(id); err == nil {
		if len(endpoints) == 0 {
			endpoints = append(endpoints, eng.Endpoints...)
		}
		if cookieA == "" { // use the engagement's session cookie as Account A
			cookieA = eng.Cookie
		}
	}
	if len(endpoints) == 0 {
		return "idor_scan: no endpoints to test. Run browser_capture first, or pass an explicit endpoints list."
	}
	if max <= 0 {
		max = 60
	}

	cid := fmt.Sprintf("c%d", time.Now().UnixNano())
	a.emit(id, model.Event{Type: model.EvCommandStarted, Data: map[string]string{
		"cid": cid, "cmdline": fmt.Sprintf("idor_scan ▶ %d endpoints (A=%v B=%v)", len(endpoints), cookieA != "", cookieB != ""),
	}})

	var sb strings.Builder
	tested, idBearing, getOnly, flagged, skipped := 0, 0, 0, 0, 0
	rateLimited := false

	for _, raw := range endpoints {
		if ctx.Err() != nil {
			break
		}
		if tested >= max {
			a.emit(id, model.Event{Type: model.EvOutputChunk, Data: map[string]string{"cid": cid, "stream": "stdout",
				"data": fmt.Sprintf("...stopped at max=%d (%d endpoints not tested)", max, len(endpoints)-tested)}})
			break
		}
		method, u := parseEndpoint(raw)
		if method != "GET" {
			skipped++
			continue // never replay state-changing verbs
		}
		tested++
		a.rateLimitWait(ctx, id)

		hasID := idRe.MatchString(u)
		if hasID {
			idBearing++
		}
		getOnly++

		pa := httpProbe(ctx, u, cookieA)
		var pb, pu probeResult
		if cookieB != "" {
			pb = httpProbe(ctx, u, cookieB)
		}
		pu = httpProbe(ctx, u, "")
		if pa.status == 429 || pb.status == 429 || pu.status == 429 {
			rateLimited = true
		}

		// STRICT confirmation: a real cross-account IDOR requires two sessions where
		// B (using A's id-bearing URL) gets A-equivalent data AND that data differs
		// from the unauthenticated baseline (proving the resource is access-controlled
		// but leaks across accounts). Anything weaker is a review note, NOT a finding.
		confirmed := hasID && cookieA != "" && cookieB != "" &&
			pa.status == 200 && pb.status == 200 &&
			similar(pa.length, pb.length) && !similar(pa.length, pu.length)

		note := ""
		switch {
		case confirmed:
			note = "CONFIRMED IDOR (B got A's data; unauth differs)"
		case hasID && cookieB != "" && pb.status == 200 && similar(pa.length, pb.length):
			note = "review: B≈A but unauth is similar/missing — NOT confirmed"
		case hasID && cookieA == "" && cookieB == "" && pu.status == 200 && pu.length > 500:
			note = "review: id-endpoint returns data with no session — verify it is private"
		}

		line := fmt.Sprintf("%-3s %s | A:%d/%d B:%d/%d U:%d/%d%s%s",
			method, truncURL(u), pa.status, pa.length, pb.status, pb.length, pu.status, pu.length,
			ifStr(hasID, " [id]", ""), ifStr(note != "", "  -> "+note, ""))
		a.emit(id, model.Event{Type: model.EvOutputChunk, Data: map[string]string{"cid": cid, "stream": ifStr(confirmed, "stderr", "stdout"), "data": line}})
		sb.WriteString(line + "\n")

		// Only a strictly-confirmed cross-account leak becomes a finding.
		if confirmed {
			flagged++
			a.recordFinding(id, model.Finding{
				Title:       "Confirmed IDOR/BOLA: cross-account data access",
				Severity:    "high",
				Confidence:  "confirmed",
				Type:        "IDOR",
				CWE:         "CWE-639",
				OWASP:       "A01:2021-Broken Access Control",
				Endpoint:    u,
				Target:      u,
				Evidence:    fmt.Sprintf("Account A: HTTP %d, %d bytes. Account B (using A's id): HTTP %d, %d bytes (≈A). Unauthenticated: HTTP %d, %d bytes (differs). B receiving A's data proves cross-account access.", pa.status, pa.length, pb.status, pb.length, pu.status, pu.length),
				Impact:      "A user can read another user's data by reusing their object id — confirmed across two accounts.",
				Remediation: "Enforce object-level authorization: verify the requested object belongs to the authenticated principal.",
				Steps:       []string{"As Account A, capture the id-bearing request.", "Replay it verbatim with Account B's session.", "Observe Account A's data returned to B."},
			})
		}
	}

	a.emit(id, model.Event{Type: model.EvCommandDone, Data: map[string]any{"cid": cid, "exit_code": 0}})

	var head strings.Builder
	fmt.Fprintf(&head, "idor_scan tested %d endpoints (%d GET, %d id-bearing, %d non-GET skipped). CONFIRMED %d (strict cross-account).\n",
		tested, getOnly, idBearing, skipped, flagged)
	if cookieA == "" || cookieB == "" {
		head.WriteString("NOTE: IDOR cannot be CONFIRMED without TWO accounts. Provide cookie_a AND cookie_b (two logged-in sessions). " +
			"Without them, lines marked 'review:' are only candidates — do NOT report them as confirmed. " +
			"Value tricks like id=0/-1/id±1 are weak signals, never proof of IDOR.\n")
	}
	if rateLimited {
		head.WriteString("NOTE: some requests returned 429 (rate limited) — consider setting a rate limit and retrying.\n")
	}
	head.WriteString("Format: METHOD url | A:status/len B:status/len U:status/len\n\n")
	out := head.String() + sb.String()
	if len(out) > a.cfg.Agent.OutputLimit {
		out = out[:a.cfg.Agent.OutputLimit] + "\n...[truncated]"
	}
	return out
}

func truncURL(u string) string {
	if len(u) > 90 {
		return u[:90] + "…"
	}
	return u
}

func ifStr(cond bool, yes, no string) string {
	if cond {
		return yes
	}
	return no
}
