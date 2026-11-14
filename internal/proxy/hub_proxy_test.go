package proxy

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"gravlink/internal/agy"
)

func TestHubProxyRewritesHTMLAndInjectsPatches(t *testing.T) {
	var seenPath string
	var seenCSRF string

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			w.WriteHeader(http.StatusOK)
			return
		}
		seenPath = r.URL.Path
		seenCSRF = r.Header.Get("x-codeium-csrf-token")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("X-Frame-Options", "DENY")
		_, _ = w.Write([]byte(`<!doctype html><html><head><script>window.__APP_CONFIG__={"csrfToken":"abc"};</script><link rel="stylesheet" href="/jetbox.css"><script src="/main.js"></script></head><body><div id="root"></div></body></html>`))
	}))
	defer upstream.Close()

	_, portStr, _ := net.SplitHostPort(upstream.Listener.Addr().String())
	port, _ := strconv.Atoi(portStr)

	agDir := t.TempDir()
	daemonDir := filepath.Join(agDir, "daemon")
	_ = os.MkdirAll(daemonDir, 0o755)
	hubJSON, _ := json.Marshal(agy.HubInfo{
		PID:       os.Getpid(),
		HTTPPort:  port,
		LSVersion: "2.31.2",
		CSRFToken: "hub-token-123",
	})
	_ = os.WriteFile(filepath.Join(daemonDir, "hub_"+portStr+".json"), hubJSON, 0o644)

	handler := NewHubProxyHandler(agDir)
	req := httptest.NewRequest(http.MethodGet, "/hub/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if seenPath != "/" || seenCSRF != "hub-token-123" {
		t.Fatalf("unexpected upstream request: path=%q csrf=%q", seenPath, seenCSRF)
	}
	if rec.Header().Get("X-Frame-Options") != "" {
		t.Fatalf("expected X-Frame-Options stripped, got %q", rec.Header().Get("X-Frame-Options"))
	}
	body := rec.Body.String()
	if !strings.Contains(body, `src="/hub/main.js"`) || !strings.Contains(body, `href="/hub/jetbox.css"`) {
		t.Fatalf("expected asset paths rewritten to /hub/, got: %s", body)
	}
	if !strings.Contains(body, `/patches/mobile-hub.js`) || !strings.Contains(body, `/patches/mobile-hub.css`) {
		t.Fatalf("expected mobile patches injected, got: %s", body)
	}
}
