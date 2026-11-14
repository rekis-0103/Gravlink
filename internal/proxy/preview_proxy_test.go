package proxy

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestIsAllowedPreviewPort(t *testing.T) {
	if IsAllowedPreviewPort(80, 8443) {
		t.Fatal("expected privileged port 80 to be disallowed")
	}
	if IsAllowedPreviewPort(8443, 8443) {
		t.Fatal("expected self port to be disallowed")
	}
	if !IsAllowedPreviewPort(5173, 8443) {
		t.Fatal("expected 5173 to be allowed")
	}
}

func TestPreviewProxyRewritesHTMLAndRejectsBadPort(t *testing.T) {
	devSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("X-Frame-Options", "DENY")
			_, _ = w.Write([]byte(`<html><head><title>App</title></head><body><script type="module" src="/@vite/client"></script></body></html>`))
		case "/redir":
			w.Header().Set("Location", "/dashboard")
			w.WriteHeader(http.StatusFound)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer devSrv.Close()

	_, portStr, _ := net.SplitHostPort(devSrv.Listener.Addr().String())
	port, _ := strconv.Atoi(portStr)

	h := NewPreviewProxyHandler(8443)

	// Disallowed privileged port
	badReq := httptest.NewRequest(http.MethodGet, "/preview/22/", nil)
	badRec := httptest.NewRecorder()
	h.ServeHTTP(badRec, badReq)
	if badRec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on port 22, got %d", badRec.Code)
	}

	// Rewrites root paths and strips X-Frame-Options
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/preview/%d/", port), nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if rec.Header().Get("X-Frame-Options") != "" {
		t.Fatal("expected upstream X-Frame-Options to be stripped")
	}
	body := rec.Body.String()
	expectedPrefix := fmt.Sprintf(`/preview/%d/`, port)
	if !strings.Contains(body, `src="`+expectedPrefix+`@vite/client"`) || !strings.Contains(body, `<base href="`+expectedPrefix+`" />`) {
		t.Fatalf("unexpected rewritten HTML: %s", body)
	}

	// Rewrites Location header on redirect
	redirReq := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/preview/%d/redir", port), nil)
	redirRec := httptest.NewRecorder()
	h.ServeHTTP(redirRec, redirReq)
	if got := redirRec.Header().Get("Location"); got != expectedPrefix+"dashboard" {
		t.Fatalf("expected Location %q, got %q", expectedPrefix+"dashboard", got)
	}
}

func TestScanLocalDevPorts(t *testing.T) {
	ports := ScanLocalDevPorts(context.Background(), 8443)
	if len(ports) == 0 {
		t.Fatal("expected candidate dev ports list")
	}
}
