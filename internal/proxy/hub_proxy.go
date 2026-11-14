package proxy

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"

	"gravlink/internal/agy"
)

// NewHubProxyHandler creates a reverse proxy for /hub/* targeting the live agy --hub server.
func NewHubProxyHandler(antigravityDir string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hubs, err := agy.DiscoverHubs(r.Context(), antigravityDir)
		if err != nil || len(hubs) == 0 {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`<!doctype html><html><body style="font-family:system-ui;background:#0f172a;color:#e2e8f0;padding:1.5rem;">
<h3>Antigravity Hub Not Running</h3>
<p>Open Antigravity IDE on your laptop to start the local hub daemon, then tap Reload.</p>
</body></html>`))
			return
		}

		hub := hubs[0]
		target, err := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", hub.HTTPPort))
		if err != nil {
			http.Error(w, "Invalid hub target", http.StatusInternalServerError)
			return
		}

		// Strip /hub prefix so upstream receives /, /main.js, /exa.*, etc.
		upstreamPath := strings.TrimPrefix(r.URL.Path, "/hub")
		if upstreamPath == "" {
			upstreamPath = "/"
		}

		proxy := &httputil.ReverseProxy{
			Director: func(req *http.Request) {
				req.URL.Scheme = target.Scheme
				req.URL.Host = target.Host
				req.URL.Path = upstreamPath
				req.URL.RawPath = ""
				req.Host = target.Host
				req.Header.Set("Origin", target.String())
				if hub.CSRFToken != "" && req.Header.Get("x-codeium-csrf-token") == "" {
					req.Header.Set("x-codeium-csrf-token", hub.CSRFToken)
				}
				// Ensure HTML comes uncompressed so ModifyResponse can patch cleanly.
				if upstreamPath == "/" || strings.HasSuffix(upstreamPath, ".html") {
					req.Header.Del("Accept-Encoding")
				}
			},
			ModifyResponse: func(resp *http.Response) error {
				resp.Header.Del("X-Frame-Options")
				resp.Header.Del("Content-Security-Policy")

				ct := strings.ToLower(resp.Header.Get("Content-Type"))
				if !strings.Contains(ct, "text/html") {
					return nil
				}

				raw, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
				if err != nil {
					return err
				}
				_ = resp.Body.Close()

				patched := patchHubHTML(string(raw), hub.HTTPPort)
				out := []byte(patched)
				resp.Body = io.NopCloser(bytes.NewReader(out))
				resp.ContentLength = int64(len(out))
				resp.Header.Set("Content-Length", strconv.Itoa(len(out)))
				return nil
			},
			ErrorHandler: func(rw http.ResponseWriter, req *http.Request, err error) {
				http.Error(rw, "Hub proxy error: "+err.Error(), http.StatusBadGateway)
			},
		}

		proxy.ServeHTTP(w, r)
	})
}

func patchHubHTML(html string, hubPort int) string {
	// Rewrite root-relative asset tags so /main.js, /jetbox.css, etc. route via /hub/
	html = strings.ReplaceAll(html, `href="/`, `href="/hub/`)
	html = strings.ReplaceAll(html, `src="/`, `src="/hub/`)
	html = strings.ReplaceAll(html, `href='/`, `href='/hub/`)
	html = strings.ReplaceAll(html, `src='/`, `src='/hub/`)

	bootShim := fmt.Sprintf(
		`<script>window.__GRAVLINK_HUB_PORT__=%d;</script><script src="/patches/mobile-hub.js"></script>`,
		hubPort,
	)
	cssPatch := `<link rel="stylesheet" href="/patches/mobile-hub.css" />`

	// Inject JS shim right after <head> (after __APP_CONFIG__ if present) so fetch/WS are patched before main.js.
	if idx := strings.Index(html, "</script>"); idx != -1 && strings.Contains(html[:idx], "__APP_CONFIG__") {
		pos := idx + len("</script>")
		html = html[:pos] + "\n" + bootShim + html[pos:]
	} else if idx := strings.Index(strings.ToLower(html), "<head>"); idx != -1 {
		pos := idx + len("<head>")
		html = html[:pos] + "\n" + bootShim + html[pos:]
	} else {
		html = bootShim + "\n" + html
	}

	if idx := strings.LastIndex(strings.ToLower(html), "</head>"); idx != -1 {
		html = html[:idx] + cssPatch + "\n" + html[idx:]
	} else {
		html += "\n" + cssPatch
	}
	return html
}
