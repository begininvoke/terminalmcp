// Package browser drives a real (headless) Chrome via the DevTools Protocol to
// load a page and capture the network requests it makes — the way to discover
// JS-rendered routes/API endpoints that a plain HTTP crawler misses. This is the
// "open it in Chrome and watch the network tab" capability, exposed to the agent.
package browser

import (
	"context"
	"net/url"
	"os"
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
}

// Config controls how Chrome is launched.
type Config struct {
	ChromePath string
	Headless   bool
	NavWait    time.Duration
	Timeout    time.Duration
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

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", cfg.Headless),
		chromedp.Flag("ignore-certificate-errors", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
	)
	if path := detectChrome(cfg.ChromePath); path != "" {
		opts = append(opts, chromedp.ExecPath(path))
	}

	allocCtx, cancelAlloc := chromedp.NewExecAllocator(parent, opts...)
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

	headers := map[string]interface{}{}
	for k, v := range extraHeaders {
		headers[k] = v
	}
	if cookieHeader != "" {
		headers["Cookie"] = cookieHeader
	}

	// Navigate.
	pre := []chromedp.Action{network.Enable()}
	if len(headers) > 0 {
		pre = append(pre, network.SetExtraHTTPHeaders(network.Headers(headers)))
	}
	pre = append(pre, chromedp.Navigate(target))
	if err := chromedp.Run(ctx, pre...); err != nil && len(reqs) == 0 {
		return nil, err
	}

	// Settle: scroll through the page for the full navWait window to trigger
	// lazy-loaded content/XHRs and give the DOM time to render. (Scrolling the
	// whole window catches far more endpoints+links than a single fixed sleep.)
	start := time.Now()
	for time.Since(start) < navWait {
		var dummy int
		_ = chromedp.Run(ctx, chromedp.Evaluate(`window.scrollBy(0, Math.max(600, window.innerHeight)); 0`, &dummy))
		select {
		case <-ctx.Done():
			start = start.Add(-navWait) // force exit
		case <-time.After(600 * time.Millisecond):
		}
	}

	// Extract anchors after settle, from the top (best effort — must not drop endpoints).
	var links []string
	_ = chromedp.Run(ctx,
		chromedp.Evaluate(`window.scrollTo(0,0); 0`, new(int)),
		chromedp.Evaluate(`Array.from(document.querySelectorAll('a[href]')).map(a=>a.href)`, &links),
	)

	mu.Lock()
	captured := reqs
	mu.Unlock()
	return buildResult(target, captured, links), nil
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
