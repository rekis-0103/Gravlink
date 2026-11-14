package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"gravlink/internal/config"
	"gravlink/internal/security"
	"gravlink/internal/workspace"
)

func setupTestServer(t *testing.T) (http.Handler, *config.Config, *security.SessionManager, string, string) {
	t.Helper()
	dataDir := t.TempDir()
	agDir := t.TempDir()
	wsDir := t.TempDir()

	if err := os.WriteFile(filepath.Join(wsDir, "hello.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wsDir, ".env"), []byte("SECRET=xyz\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	convDir := filepath.Join(agDir, "brain", "test-conv", ".system_generated", "logs")
	if err := os.MkdirAll(convDir, 0o755); err != nil {
		t.Fatal(err)
	}
	transcriptPath := filepath.Join(convDir, "transcript.jsonl")
	if err := os.WriteFile(transcriptPath, []byte(`{"step_index":0,"source":"USER_EXPLICIT","type":"USER_INPUT","status":"DONE","created_at":"2026-11-14T05:38:43Z","content":"hello"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	hash, err := security.HashPassword("supersecret123")
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		Port:            8443,
		Workspace:       wsDir,
		AntigravityDir:  agDir,
		DataDir:         dataDir,
		PasswordHash:    hash,
		SessionSecret:   "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff",
		UseTLS:          false,
		StrictSubnet:    true,
		SessionTTLHours: 24,
	}
	sm, err := security.NewSessionManager(cfg.SessionSecret, time.Hour, false)
	if err != nil {
		t.Fatal(err)
	}
	audit, err := security.NewAuditLogger(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = audit.Close() })

	exp, err := workspace.NewExplorer(wsDir)
	if err != nil {
		t.Fatal(err)
	}
	_, lanNet, _ := net.ParseCIDR("192.168.1.0/24")

	h := NewRouter(Dependencies{
		Config:        cfg,
		Sessions:      sm,
		RateLimiter:   security.NewAuthRateLimiter(5, time.Minute),
		Audit:         audit,
		Explorer:      exp,
		StrictSubnets: []*net.IPNet{lanNet},
	})
	return h, cfg, sm, transcriptPath, wsDir
}

func TestRouterAuthAndSecurityBoundaries(t *testing.T) {
	handler, _, _, _, _ := setupTestServer(t)

	// 1. Public IP rejected with 403
	req := httptest.NewRequest(http.MethodGet, "/api/auth/status", nil)
	req.RemoteAddr = "8.8.8.8:1234"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for public IP, got %d", rec.Code)
	}

	// 2. Unauthenticated request to protected route returns 401
	req = httptest.NewRequest(http.MethodGet, "/api/conversations", nil)
	req.RemoteAddr = "192.168.1.20:5555"
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without session, got %d", rec.Code)
	}

	// 3. Rate limiting on wrong passwords
	for i := 0; i < 5; i++ {
		badReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewBufferString(`{"password":"wrong"}`))
		badReq.RemoteAddr = "192.168.1.20:5555"
		badRec := httptest.NewRecorder()
		handler.ServeHTTP(badRec, badReq)
		if badRec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d expected 401, got %d", i, badRec.Code)
		}
	}
	blockedReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewBufferString(`{"password":"supersecret123"}`))
	blockedReq.RemoteAddr = "192.168.1.20:5555"
	blockedRec := httptest.NewRecorder()
	handler.ServeHTTP(blockedRec, blockedReq)
	if blockedRec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 after 5 failures, got %d", blockedRec.Code)
	}

	// 4. Valid login from another LAN IP succeeds and returns cookie + allows file read
	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewBufferString(`{"password":"supersecret123"}`))
	loginReq.RemoteAddr = "192.168.1.21:5555"
	loginRec := httptest.NewRecorder()
	handler.ServeHTTP(loginRec, loginReq)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("expected 200 on valid login, got %d", loginRec.Code)
	}
	cookie := loginRec.Result().Cookies()[0]

	fileReq := httptest.NewRequest(http.MethodGet, "/api/workspace/file?path=hello.go", nil)
	fileReq.RemoteAddr = "192.168.1.21:5555"
	fileReq.AddCookie(cookie)
	fileRec := httptest.NewRecorder()
	handler.ServeHTTP(fileRec, fileReq)
	if fileRec.Code != http.StatusOK {
		t.Fatalf("expected 200 for hello.go, got %d", fileRec.Code)
	}

	// 5. .env blocked with 403 even when authenticated
	envReq := httptest.NewRequest(http.MethodGet, "/api/workspace/file?path=.env", nil)
	envReq.RemoteAddr = "192.168.1.21:5555"
	envReq.AddCookie(cookie)
	envRec := httptest.NewRecorder()
	handler.ServeHTTP(envRec, envReq)
	if envRec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for .env, got %d", envRec.Code)
	}
}

func TestRouterWebSocketRealtimeStreaming(t *testing.T) {
	handler, _, sm, transcriptPath, _ := setupTestServer(t)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	tok, err := sm.CreatePairingToken(time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	pairRes, err := http.Post(srv.URL+"/api/auth/pair", "application/json", strings.NewReader(`{"token":"`+tok+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer pairRes.Body.Close()
	if pairRes.StatusCode != http.StatusOK {
		t.Fatalf("pair failed with %d", pairRes.StatusCode)
	}
	cookies := pairRes.Cookies()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/ws/conversations/test-conv"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPHeader: http.Header{"Cookie": []string{cookies[0].Name + "=" + cookies[0].Value}},
	})
	if err != nil {
		t.Fatalf("websocket dial failed: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "done")

	_, msg1, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read snapshot failed: %v", err)
	}
	var ev1 wsEvent
	if err := json.Unmarshal(msg1, &ev1); err != nil || ev1.Type != "snapshot" || len(ev1.Steps) != 1 {
		t.Fatalf("unexpected snapshot event: %s", string(msg1))
	}

	// Append a new step and verify delta arrives automatically
	f, err := os.OpenFile(transcriptPath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString(`{"step_index":1,"source":"MODEL","type":"PLANNER_RESPONSE","status":"DONE","created_at":"2026-11-14T05:38:46Z","content":"streamed live"}` + "\n")
	_ = f.Close()

	_, msg2, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read delta failed: %v", err)
	}
	var ev2 wsEvent
	if err := json.Unmarshal(msg2, &ev2); err != nil || ev2.Type != "delta" || len(ev2.Steps) != 1 || ev2.Steps[0].Content != "streamed live" {
		t.Fatalf("unexpected delta event: %s", string(msg2))
	}
}
