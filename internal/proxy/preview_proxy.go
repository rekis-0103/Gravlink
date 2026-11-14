package proxy

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DevServerPort describes a local localhost port probed for web preview.
type DevServerPort struct {
	Port   int    `json:"port"`
	Label  string `json:"label"`
	Active bool   `json:"active"`
}

var commonDevPorts = []DevServerPort{
	{Port: 5173, Label: "Vite"},
	{Port: 5174, Label: "Vite Alt"},
	{Port: 3000, Label: "React / Next"},
	{Port: 3001, Label: "Node Alt"},
	{Port: 4173, Label: "Vite Preview"},
	{Port: 4200, Label: "Angular"},
	{Port: 4321, Label: "Astro"},
	{Port: 5000, Label: "Flask / API"},
	{Port: 8000, Label: "FastAPI / Django"},
	{Port: 8080, Label: "Go / HTTP"},
	{Port: 8888, Label: "Jupyter / Web"},
	{Port: 9000, Label: "Dev Server"},
}

// ScanLocalDevPorts probes common localhost dev ports concurrently.
func ScanLocalDevPorts(ctx context.Context, excludePort int) []DevServerPort {
	results := make([]DevServerPort, len(commonDevPorts))
	var wg sync.WaitGroup
	dialer := &net.Dialer{Timeout: 120 * time.Millisecond}

	for i, candidate := range commonDevPorts {
		results[i] = candidate
		if candidate.Port == excludePort {
			continue
		}
		wg.Add(1)
		go func(idx, p int) {
			defer wg.Done()
			probeCtx, cancel := context.WithTimeout(ctx, 150*time.Millisecond)
			defer cancel()
			conn, err := dialer.DialContext(probeCtx, "tcp", fmt.Sprintf("127.0.0.1:%d", p))
			if err == nil {
				_ = conn.Close()
				results[idx].Active = true
			}
		}(i, candidate.Port)
	}
	wg.Wait()
	return results
}

// IsAllowedPreviewPort restricts preview proxying to localhost user ports, excluding Gravlink itself.
func IsAllowedPreviewPort(port, selfPort int) bool {
	if port < 1024 || port > 65535 {
		return false
	}
	if selfPort > 0 && port == selfPort {
		return false
	}
	return true
}

// NewPreviewProxyHandler creates a LAN-authenticated dynamic reverse proxy for /preview/<port>/*.
func NewPreviewProxyHandler(selfPort int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		port, upstreamPath, ok := parsePreviewRoute(r.URL.Path)
		if !ok || !IsAllowedPreviewPort(port, selfPort) {
			http.Error(w, "Invalid or disallowed preview port", http.StatusBadRequest)
			return
		}

		target, err := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", port))
		if err != nil {
			http.Error(w, "Invalid preview target", http.StatusInternalServerError)
			return
		}

		prefix := fmt.Sprintf("/preview/%d", port)
		rp := &httputil.ReverseProxy{
			FlushInterval: -1, // Support streaming HMR / WebSocket / SSE cleanly
			Director: func(req *http.Request) {
				req.URL.Scheme = target.Scheme
				req.URL.Host = target.Host
				req.URL.Path = upstreamPath
				req.URL.RawPath = ""
				req.Host = target.Host
				req.Header.Set("Origin", target.String())

				if upstreamPath == "/" || strings.HasSuffix(upstreamPath, ".html") ||
					strings.Contains(req.Header.Get("Accept"), "text/html") {
					req.Header.Del("Accept-Encoding")
				}
			},
			ModifyResponse: func(resp *http.Response) error {
				resp.Header.Del("X-Frame-Options")
				resp.Header.Del("Content-Security-Policy")

				if loc := resp.Header.Get("Location"); loc != "" {
					resp.Header.Set("Location", rewritePreviewLocation(loc, port, prefix))
				}

				ct := strings.ToLower(resp.Header.Get("Content-Type"))
				if !strings.Contains(ct, "text/html") {
					return nil
				}

				raw, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
				if err != nil {
					return err
				}
				_ = resp.Body.Close()

				patched := patchPreviewHTML(string(raw), prefix)
				out := []byte(patched)
				resp.Body = io.NopCloser(bytes.NewReader(out))
				resp.ContentLength = int64(len(out))
				resp.Header.Set("Content-Length", strconv.Itoa(len(out)))
				return nil
			},
			ErrorHandler: func(rw http.ResponseWriter, req *http.Request, err error) {
				rw.Header().Set("Content-Type", "text/html; charset=utf-8")
				rw.WriteHeader(http.StatusBadGateway)
				_, _ = fmt.Fprintf(rw, `<!doctype html><html><body style="font-family:system-ui;background:#0f172a;color:#e2e8f0;padding:1.5rem;">
<h3>No Dev Server on 127.0.0.1:%d</h3>
<p>Start your local web dev server on port <strong>%d</strong>, then tap Reload.</p>
<pre style="font-size:0.8rem;color:#94a3b8;white-space:pre-wrap;">%s</pre>
</body></html>`, port, port, err.Error())
			},
		}

		rp.ServeHTTP(w, r)
	})
}

func parsePreviewRoute(urlPath string) (port int, upstreamPath string, ok bool) {
	trimmed := strings.TrimPrefix(urlPath, "/preview/")
	if trimmed == urlPath || trimmed == "" {
		return 0, "", false
	}
	slashIdx := strings.IndexByte(trimmed, '/')
	var portStr string
	if slashIdx == -1 {
		portStr = trimmed
		upstreamPath = "/"
	} else {
		portStr = trimmed[:slashIdx]
		upstreamPath = trimmed[slashIdx:]
		if upstreamPath == "" {
			upstreamPath = "/"
		}
	}
	p, err := strconv.Atoi(portStr)
	if err != nil || p <= 0 {
		return 0, "", false
	}
	return p, upstreamPath, true
}

func rewritePreviewLocation(loc string, port int, prefix string) string {
	for _, host := range []string{
		fmt.Sprintf("http://127.0.0.1:%d", port),
		fmt.Sprintf("http://localhost:%d", port),
	} {
		if strings.HasPrefix(loc, host) {
			rest := strings.TrimPrefix(loc, host)
			if rest == "" {
				return prefix + "/"
			}
			return prefix + rest
		}
	}
	if strings.HasPrefix(loc, "/") && !strings.HasPrefix(loc, prefix) {
		return prefix + loc
	}
	return loc
}

func patchPreviewHTML(html string, prefix string) string {
	html = strings.ReplaceAll(html, `href="/`, `href="`+prefix+`/`)
	html = strings.ReplaceAll(html, `src="/`, `src="`+prefix+`/`)
	html = strings.ReplaceAll(html, `href='/`, `href='`+prefix+`/`)
	html = strings.ReplaceAll(html, `src='/`, `src='`+prefix+`/`)
	// Avoid double-prefixing if upstream already used /preview/<port>/
	html = strings.ReplaceAll(html, prefix+prefix+`/`, prefix+`/`)

	shim := fmt.Sprintf(`<base href="%s/" />
<script>
(function(){
  var P = %q;
  function rw(u){
    if (typeof u !== 'string') return u;
    if (u.startsWith('/') && !u.startsWith('//') && !u.startsWith(P + '/') && u !== P) return P + u;
    return u;
  }
  var origFetch = window.fetch;
  if (origFetch) {
    window.fetch = function(input, init) {
      if (typeof input === 'string') return origFetch.call(this, rw(input), init);
      if (input && typeof input.url === 'string') {
        var u = new URL(input.url, location.href);
        if (u.origin === location.origin && !u.pathname.startsWith(P + '/')) {
          return origFetch.call(this, new Request(P + u.pathname + u.search, input), init);
        }
      }
      return origFetch.call(this, input, init);
    };
  }
  var OrigWS = window.WebSocket;
  if (OrigWS) {
    window.WebSocket = function(url, protocols) {
      try {
        var parsed = new URL(url, location.href);
        if ((parsed.host === location.host || parsed.hostname === '127.0.0.1' || parsed.hostname === 'localhost') &&
            !parsed.pathname.startsWith(P + '/')) {
          var proto = location.protocol === 'https:' ? 'wss://' : 'ws://';
          url = proto + location.host + P + parsed.pathname + parsed.search;
        }
      } catch(e) {}
      return protocols ? new OrigWS(url, protocols) : new OrigWS(url);
    };
    window.WebSocket.prototype = OrigWS.prototype;
  }
})();
</script>`, prefix, prefix)

	if idx := strings.Index(strings.ToLower(html), "<head>"); idx != -1 {
		pos := idx + len("<head>")
		return html[:pos] + "\n" + shim + html[pos:]
	}
	return shim + "\n" + html
}
