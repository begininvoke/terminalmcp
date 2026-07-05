// Package browser drives a real (headless) Chrome via the DevTools Protocol to
// load a page and capture the network requests it makes — the way to discover
// JS-rendered routes/API endpoints that a plain HTTP crawler misses. This is the
// "open it in Chrome and watch the network tab" capability, exposed to the agent.
package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

// Request is one captured network request.
type Request struct {
	Method string `json:"method"`
	URL    string `json:"url"`
	Type   string `json:"type"` // Document, XHR, Fetch, Script, ...
}

// Result is the outcome of a capture.
type Result struct {
	URL       string    `json:"url"`
	Requests  []Request `json:"requests"`
	Endpoints []string  `json:"endpoints"` // deduped same-origin API/doc/script URLs
	Links     []string  `json:"links"`     // same-origin anchor hrefs
	Total     int       `json:"total"`
	Cookies   string    `json:"-"` // "k=v; k2=v2" Cookie header for the target, read from the live browser session (never persisted)
}

// Config controls how Chrome is launched.
type Config struct {
	ChromePath  string
	Headless    bool
	NavWait     time.Duration
	Timeout     time.Duration
	RemoteURL   string // if set (e.g. http://localhost:9222), attach to a real Chrome instead of launching headless
	AutoLaunch  bool   // if RemoteURL is unreachable, start a persistent debug Chrome and attach to it
	UserDataDir string // profile dir for the auto-launched debug Chrome (persists logins across runs)
}

// ensureDebugChrome makes sure a Chrome with the debug port is running; if not,
// it launches a persistent one (own profile) so it survives across captures.
func ensureDebugChrome(remoteURL, chromePath, userDataDir string) error {
	if _, err := remoteWSURL(remoteURL); err == nil {
		return nil // already up
	}
	path := detectChrome(chromePath)
	if path == "" {
		return fmt.Errorf("no Chrome/Chromium binary found to launch")
	}
	port := "9222"
	if u, err := url.Parse(remoteURL); err == nil && u.Port() != "" {
		port = u.Port()
	}
	if userDataDir == "" {
		userDataDir = filepath.Join(os.TempDir(), "pentest-chrome")
	}
	cmd := exec.Command(path,
		"--remote-debugging-port="+port,
		"--remote-allow-origins=*", // required by Chrome 111+ for CDP websocket connections
		"--user-data-dir="+userDataDir,
		"--no-first-run", "--no-default-browser-check",
		"about:blank",
	)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("launch debug Chrome: %w", err)
	}
	go cmd.Wait() // reap when it eventually exits; do not block
	for i := 0; i < 24; i++ {
		time.Sleep(500 * time.Millisecond)
		if _, err := remoteWSURL(remoteURL); err == nil {
			return nil
		}
	}
	return fmt.Errorf("debug Chrome did not expose %s in time", remoteURL)
}

// remoteWSURL resolves a CDP websocket endpoint from an http debug URL.
func remoteWSURL(httpURL string) (string, error) {
	u := strings.TrimRight(httpURL, "/")
	if strings.HasPrefix(u, "ws://") || strings.HasPrefix(u, "wss://") {
		return u, nil
	}
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Get(u + "/json/version")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var v struct {
		WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		return "", err
	}
	if v.WebSocketDebuggerURL == "" {
		return "", fmt.Errorf("no webSocketDebuggerUrl at %s", u)
	}
	return v.WebSocketDebuggerURL, nil
}

// detectChrome finds a Chrome/Chromium binary if none was configured.
func detectChrome(p string) string {
	if p != "" {
		return p
	}
	// Prefer real Chrome/Chromium executables. Skip homebrew's /opt/homebrew/bin
	// /chromium — it is a shell-script wrapper that chromedp cannot manage.
	for _, c := range []string{
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		"/Applications/Chromium.app/Contents/MacOS/Chromium",
		"/usr/bin/google-chrome",
		"/usr/bin/chromium",
	} {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return ""
}

// Capture loads url in headless Chrome, records every network request, and
// extracts same-origin endpoints. cookieHeader/extraHeaders allow authenticated
// captures (e.g. testing IDOR while logged in).
func Capture(parent context.Context, cfg Config, target, cookieHeader string, extraHeaders map[string]string) (*Result, error) {
	navWait := cfg.NavWait
	if navWait <= 0 {
		navWait = 6 * time.Second
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}

	headers := map[string]interface{}{}
	for k, v := range extraHeaders {
		headers[k] = v
	}
	if cookieHeader != "" {
		headers["Cookie"] = cookieHeader
	}

	// One capture attempt against a given allocator. Returns an error if the page
	// never loaded (e.g. the -32000 "no browser open" when remote attach is stale).
	attempt := func(allocCtx context.Context, cancelAlloc context.CancelFunc) (*Result, error) {
		defer cancelAlloc()
		ctx, cancelCtx := chromedp.NewContext(allocCtx)
		defer cancelCtx()
		ctx, cancelTimeout := context.WithTimeout(ctx, timeout)
		defer cancelTimeout()

		var mu sync.Mutex
		var reqs []Request
		chromedp.ListenTarget(ctx, func(ev interface{}) {
			if e, ok := ev.(*network.EventRequestWillBeSent); ok {
				mu.Lock()
				reqs = append(reqs, Request{Method: e.Request.Method, URL: e.Request.URL, Type: e.Type.String()})
				mu.Unlock()
			}
		})

		pre := []chromedp.Action{network.Enable()}
		if len(headers) > 0 {
			pre = append(pre, network.SetExtraHTTPHeaders(network.Headers(headers)))
		}
		pre = append(pre, chromedp.Navigate(target))
		if err := chromedp.Run(ctx, pre...); err != nil {
			mu.Lock()
			n := len(reqs)
			mu.Unlock()
			if n == 0 {
				return nil, err // nothing captured -> real failure, let caller fall back
			}
		}

		start := time.Now()
		for time.Since(start) < navWait {
			var dummy int
			_ = chromedp.Run(ctx, chromedp.Evaluate(`window.scrollBy(0, Math.max(600, window.innerHeight)); 0`, &dummy))
			select {
			case <-ctx.Done():
				start = start.Add(-navWait)
			case <-time.After(600 * time.Millisecond):
			}
		}
		var links []string
		_ = chromedp.Run(ctx,
			chromedp.Evaluate(`window.scrollTo(0,0); 0`, new(int)),
			chromedp.Evaluate(`Array.from(document.querySelectorAll('a[href]')).map(a=>a.href)`, &links),
		)
		// Read the live session cookies for the target so terminal scanners
		// (curl/ffuf) can reuse the same authenticated session the browser has.
		var cookieHdr string
		_ = chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
			cs, err := network.GetCookies().WithURLs([]string{target}).Do(ctx)
			if err != nil {
				return err
			}
			var parts []string
			seen := map[string]bool{}
			for _, ck := range cs {
				if ck.Name == "" || seen[ck.Name] {
					continue
				}
				seen[ck.Name] = true
				parts = append(parts, ck.Name+"="+ck.Value)
			}
			cookieHdr = strings.Join(parts, "; ")
			return nil
		}))
		mu.Lock()
		captured := reqs
		mu.Unlock()
		res := buildResult(target, captured, links)
		res.Cookies = cookieHdr
		return res, nil
	}

	// Prefer attaching to a real Chrome via its debug port; if that fails (e.g.
	// -32000), fall back to a headless instance so capture never dead-ends.
	if cfg.RemoteURL != "" {
		if cfg.AutoLaunch {
			_ = ensureDebugChrome(cfg.RemoteURL, cfg.ChromePath, cfg.UserDataDir)
		}
		if ws, err := remoteWSURL(cfg.RemoteURL); err == nil {
			a, c := chromedp.NewRemoteAllocator(parent, ws)
			if res, err := attempt(a, c); err == nil {
				return res, nil
			}
			// else: remote attach failed — fall through to headless
		}
	}
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("ignore-certificate-errors", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
	)
	if path := detectChrome(cfg.ChromePath); path != "" {
		opts = append(opts, chromedp.ExecPath(path))
	}
	a, c := chromedp.NewExecAllocator(parent, opts...)
	return attempt(a, c)
}

// registrableDomain returns the last two labels of a host (e.g. api.digikala.com
// -> digikala.com), so requests to sibling subdomains (the API host) are kept.
func registrableDomain(host string) string {
	parts := strings.Split(host, ".")
	if len(parts) <= 2 {
		return host
	}
	return strings.Join(parts[len(parts)-2:], ".")
}

var assetExts = []string{".css", ".js", ".map", ".png", ".jpg", ".jpeg", ".gif", ".svg",
	".woff", ".woff2", ".ttf", ".ico", ".webp", ".avif", ".mp4", ".webm"}

func isAsset(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	p := strings.ToLower(u.Path)
	for _, ext := range assetExts {
		if strings.HasSuffix(p, ext) {
			return true
		}
	}
	return false
}

func buildResult(target string, reqs []Request, links []string) *Result {
	base, _ := url.Parse(target)
	baseDomain := ""
	if base != nil {
		baseDomain = registrableDomain(base.Hostname())
	}
	sameSite := func(raw string) bool {
		u, err := url.Parse(raw)
		return err == nil && (baseDomain == "" || registrableDomain(u.Hostname()) == baseDomain)
	}

	// Endpoints = the routes/APIs worth testing: XHR/fetch/document, on-site,
	// excluding static assets (some frameworks fetch CSS/JS via fetch/preload).
	epSet := map[string]bool{}
	for _, r := range reqs {
		t := strings.ToLower(r.Type)
		if (t == "xhr" || t == "fetch" || t == "document") && sameSite(r.URL) && !isAsset(r.URL) {
			epSet[r.Method+" "+r.URL] = true
		}
	}
	endpoints := make([]string, 0, len(epSet))
	for e := range epSet {
		endpoints = append(endpoints, e)
	}
	sort.Strings(endpoints)

	linkSet := map[string]bool{}
	for _, l := range links {
		if sameSite(l) {
			linkSet[l] = true
		}
	}
	uniqLinks := make([]string, 0, len(linkSet))
	for l := range linkSet {
		uniqLinks = append(uniqLinks, l)
	}
	sort.Strings(uniqLinks)

	return &Result{URL: target, Requests: reqs, Endpoints: endpoints, Links: uniqLinks, Total: len(reqs)}
}
