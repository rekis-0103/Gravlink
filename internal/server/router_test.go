package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/coder/websocket"

	"gravlink/internal/agy"
	"gravlink/internal/config"
	"gravlink/internal/proxy"
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

func TestRouterMilestone2PromptApprovalAndHubProxy(t *testing.T) {
	mockHub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			w.WriteHeader(http.StatusOK)
		case "/":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(`<html><head></head><body><script src="/main.js"></script></body></html>`))
		case "/exa.language_server_pb.LanguageServerService/GetAllCascadeTrajectories":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"trajectorySummaries": map[string]any{
					"test-conv": map[string]any{
						"summary":      "Live Hub Title",
						"stepCount":    1,
						"trajectoryId": "traj-xyz",
						"status":       "CASCADE_RUN_STATUS_WAITING_FOR_USER",
					},
				},
			})
		case "/exa.language_server_pb.LanguageServerService/GetCascadeTrajectorySteps":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"steps": []map[string]any{
					{
						"status": "CORTEX_STEP_STATUS_WAITING",
						"metadata": map[string]any{
							"sourceTrajectoryStepInfo": map[string]any{"trajectoryId": "traj-xyz"},
							"toolCall":                 map[string]any{"name": "run_command"},
							"requestedInteraction":     map[string]any{"runCommand": map[string]any{}},
						},
						"runCommand": map[string]any{"commandLine": "go test ./..."},
					},
				},
			})
		case "/exa.language_server_pb.LanguageServerService/SendUserCascadeMessage":
			_ = json.NewEncoder(w).Encode(map[string]any{})
		case "/exa.language_server_pb.LanguageServerService/HandleCascadeUserInteraction":
			_ = json.NewEncoder(w).Encode(map[string]any{})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer mockHub.Close()

	_, portStr, _ := net.SplitHostPort(mockHub.Listener.Addr().String())
	port, _ := strconv.Atoi(portStr)

	dataDir := t.TempDir()
	agDir := t.TempDir()
	wsDir := t.TempDir()
	daemonDir := filepath.Join(agDir, "daemon")
	_ = os.MkdirAll(daemonDir, 0o755)
	hubJSON, _ := json.Marshal(agy.HubInfo{
		PID:       os.Getpid(),
		HTTPPort:  port,
		LSVersion: "2.31.2",
		CSRFToken: "hub-tok",
	})
	_ = os.WriteFile(filepath.Join(daemonDir, "hub_"+portStr+".json"), hubJSON, 0o644)

	convDir := filepath.Join(agDir, "brain", "test-conv", ".system_generated", "logs")
	_ = os.MkdirAll(convDir, 0o755)
	_ = os.WriteFile(filepath.Join(convDir, "transcript.jsonl"), []byte(`{"step_index":0,"source":"USER_EXPLICIT","type":"USER_INPUT","status":"DONE","created_at":"2026-11-14T05:38:43Z","content":"hi"}`+"\n"), 0o644)

	hash, _ := security.HashPassword("supersecret123")
	cfg := &config.Config{
		Port:            8443,
		Workspace:       wsDir,
		AntigravityDir:  agDir,
		DataDir:         dataDir,
		PasswordHash:    hash,
		SessionSecret:   "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff",
		SessionTTLHours: 24,
	}
	sm, _ := security.NewSessionManager(cfg.SessionSecret, time.Hour, false)
	audit, _ := security.NewAuditLogger(dataDir)
	t.Cleanup(func() { _ = audit.Close() })
	exp, _ := workspace.NewExplorer(wsDir)
	_, lanNet, _ := net.ParseCIDR("192.168.1.0/24")

	handler := NewRouter(Dependencies{
		Config:        cfg,
		Sessions:      sm,
		RateLimiter:   security.NewAuthRateLimiter(5, time.Minute),
		Audit:         audit,
		Explorer:      exp,
		Dispatcher:    agy.NewDispatcher(agDir),
		HubProxy:      proxy.NewHubProxyHandler(agDir),
		StrictSubnets: []*net.IPNet{lanNet},
	})

	// Unauthenticated /hub/ and prompt MUST fail with 401 and set SAMEORIGIN
	unauthHub := httptest.NewRequest(http.MethodGet, "/hub/", nil)
	unauthHub.RemoteAddr = "192.168.1.30:1111"
	unauthHubRec := httptest.NewRecorder()
	handler.ServeHTTP(unauthHubRec, unauthHub)
	if unauthHubRec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on unauthenticated /hub/, got %d", unauthHubRec.Code)
	}
	if got := unauthHubRec.Header().Get("X-Frame-Options"); got != "SAMEORIGIN" {
		t.Fatalf("expected X-Frame-Options=SAMEORIGIN, got %q", got)
	}

	unauthPrompt := httptest.NewRequest(http.MethodPost, "/api/conversations/test-conv/prompt", strings.NewReader(`{"prompt":"hi"}`))
	unauthPrompt.RemoteAddr = "192.168.1.30:1111"
	unauthPromptRec := httptest.NewRecorder()
	handler.ServeHTTP(unauthPromptRec, unauthPrompt)
	if unauthPromptRec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on unauthenticated prompt, got %d", unauthPromptRec.Code)
	}

	// Authenticate
	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"password":"supersecret123"}`))
	loginReq.RemoteAddr = "192.168.1.30:1111"
	loginRec := httptest.NewRecorder()
	handler.ServeHTTP(loginRec, loginReq)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("login failed: %d", loginRec.Code)
	}
	var loginBody struct {
		CSRFToken string `json:"csrf_token"`
	}
	_ = json.NewDecoder(loginRec.Body).Decode(&loginBody)
	cookie := loginRec.Result().Cookies()[0]

	// Prompt without CSRF token MUST fail with 403
	noCSRFReq := httptest.NewRequest(http.MethodPost, "/api/conversations/test-conv/prompt", strings.NewReader(`{"prompt":"test"}`))
	noCSRFReq.RemoteAddr = "192.168.1.30:1111"
	noCSRFReq.AddCookie(cookie)
	noCSRFRec := httptest.NewRecorder()
	handler.ServeHTTP(noCSRFRec, noCSRFReq)
	if noCSRFRec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 without CSRF token, got %d", noCSRFRec.Code)
	}

	// Prompt with CSRF succeeds
	promptReq := httptest.NewRequest(http.MethodPost, "/api/conversations/test-conv/prompt", strings.NewReader(`{"prompt":"run tests"}`))
	promptReq.RemoteAddr = "192.168.1.30:1111"
	promptReq.AddCookie(cookie)
	promptReq.Header.Set(security.CSRFHeaderName, loginBody.CSRFToken)
	promptRec := httptest.NewRecorder()
	handler.ServeHTTP(promptRec, promptReq)
	if promptRec.Code != http.StatusOK {
		t.Fatalf("expected 200 on prompt, got %d (%s)", promptRec.Code, promptRec.Body.String())
	}

	// Pending interactions + approval
	pendReq := httptest.NewRequest(http.MethodGet, "/api/conversations/test-conv/pending", nil)
	pendReq.RemoteAddr = "192.168.1.30:1111"
	pendReq.AddCookie(cookie)
	pendRec := httptest.NewRecorder()
	handler.ServeHTTP(pendRec, pendReq)
	if pendRec.Code != http.StatusOK || !strings.Contains(pendRec.Body.String(), "go test ./...") {
		t.Fatalf("expected pending command in %s (status %d)", pendRec.Body.String(), pendRec.Code)
	}

	intReq := httptest.NewRequest(http.MethodPost, "/api/conversations/test-conv/interact", strings.NewReader(`{"trajectory_id":"traj-xyz","step_index":0,"kind":"runCommand","allow":true,"command_line":"go test ./..."}`))
	intReq.RemoteAddr = "192.168.1.30:1111"
	intReq.AddCookie(cookie)
	intReq.Header.Set(security.CSRFHeaderName, loginBody.CSRFToken)
	intRec := httptest.NewRecorder()
	handler.ServeHTTP(intRec, intReq)
	if intRec.Code != http.StatusOK {
		t.Fatalf("expected 200 on interact, got %d (%s)", intRec.Code, intRec.Body.String())
	}

	// Authenticated Hub proxy HTML rewrite
	hubReq := httptest.NewRequest(http.MethodGet, "/hub/", nil)
	hubReq.RemoteAddr = "192.168.1.30:1111"
	hubReq.AddCookie(cookie)
	hubRec := httptest.NewRecorder()
	handler.ServeHTTP(hubRec, hubReq)
	if hubRec.Code != http.StatusOK || !strings.Contains(hubRec.Body.String(), `/hub/main.js`) || !strings.Contains(hubRec.Body.String(), `/patches/mobile-hub.js`) {
		t.Fatalf("expected rewritten Hub HTML, got status=%d body=%s", hubRec.Code, hubRec.Body.String())
	}

	// Audit log recorded prompt_submitted & interaction_approved
	auditBytes, err := os.ReadFile(filepath.Join(dataDir, "audit.log"))
	if err != nil {
		t.Fatal(err)
	}
	auditText := string(auditBytes)
	if !strings.Contains(auditText, `"prompt_submitted"`) || !strings.Contains(auditText, `"interaction_approved"`) {
		t.Fatalf("expected prompt_submitted & interaction_approved in audit log: %s", auditText)
	}
}

func TestRouterMilestone3And4PreviewAndExternalPatches(t *testing.T) {
	devSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><head></head><body><script src="/main.tsx"></script></body></html>`))
	}))
	defer devSrv.Close()

	_, devPortStr, _ := net.SplitHostPort(devSrv.Listener.Addr().String())
	devPort, _ := strconv.Atoi(devPortStr)

	dataDir := t.TempDir()
	agDir := t.TempDir()
	wsDir := t.TempDir()
	patchDir := filepath.Join(dataDir, "patches")
	_ = os.MkdirAll(patchDir, 0o755)

	hash, _ := security.HashPassword("supersecret123")
	cfg := &config.Config{
		Port:            8443,
		Workspace:       wsDir,
		AntigravityDir:  agDir,
		DataDir:         dataDir,
		PasswordHash:    hash,
		SessionSecret:   "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff",
		SessionTTLHours: 24,
	}
	sm, _ := security.NewSessionManager(cfg.SessionSecret, time.Hour, false)
	audit, _ := security.NewAuditLogger(dataDir)
	t.Cleanup(func() { _ = audit.Close() })
	exp, _ := workspace.NewExplorer(wsDir)
	_, lanNet, _ := net.ParseCIDR("192.168.1.0/24")

	embeddedFS := fstest.MapFS{
		"patches/mobile-hub.css": {Data: []byte("/* embedded css */")},
		"patches/mobile-hub.js":  {Data: []byte("// embedded js")},
	}

	handler := NewRouter(Dependencies{
		Config:        cfg,
		Sessions:      sm,
		RateLimiter:   security.NewAuthRateLimiter(5, time.Minute),
		Audit:         audit,
		Explorer:      exp,
		PatchDir:      patchDir,
		StrictSubnets: []*net.IPNet{lanNet},
		StaticFS:      embeddedFS,
	})

	// 1. Preview requires auth
	unauthPreview := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/preview/%d/", devPort), nil)
	unauthPreview.RemoteAddr = "192.168.1.40:2222"
	unauthRec := httptest.NewRecorder()
	handler.ServeHTTP(unauthRec, unauthPreview)
	if unauthRec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on unauthenticated preview, got %d", unauthRec.Code)
	}

	// 2. Authenticate
	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"password":"supersecret123"}`))
	loginReq.RemoteAddr = "192.168.1.40:2222"
	loginRec := httptest.NewRecorder()
	handler.ServeHTTP(loginRec, loginReq)
	cookie := loginRec.Result().Cookies()[0]

	// 3. Preview ports API
	portsReq := httptest.NewRequest(http.MethodGet, "/api/preview/ports", nil)
	portsReq.RemoteAddr = "192.168.1.40:2222"
	portsReq.AddCookie(cookie)
	portsRec := httptest.NewRecorder()
	handler.ServeHTTP(portsRec, portsReq)
	if portsRec.Code != http.StatusOK || !strings.Contains(portsRec.Body.String(), `"ports"`) {
		t.Fatalf("expected ports JSON, got %d (%s)", portsRec.Code, portsRec.Body.String())
	}

	// 4. Authenticated preview rewrites HTML
	prevReq := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/preview/%d/", devPort), nil)
	prevReq.RemoteAddr = "192.168.1.40:2222"
	prevReq.AddCookie(cookie)
	prevRec := httptest.NewRecorder()
	handler.ServeHTTP(prevRec, prevReq)
	if prevRec.Code != http.StatusOK || !strings.Contains(prevRec.Body.String(), fmt.Sprintf(`/preview/%d/main.tsx`, devPort)) {
		t.Fatalf("expected proxied & rewritten preview HTML, got %d body=%s", prevRec.Code, prevRec.Body.String())
	}

	// 5. Patch fallback -> embedded first
	patchReq := httptest.NewRequest(http.MethodGet, "/patches/mobile-hub.css", nil)
	patchReq.RemoteAddr = "192.168.1.40:2222"
	patchRec := httptest.NewRecorder()
	handler.ServeHTTP(patchRec, patchReq)
	if patchRec.Code != http.StatusOK || patchRec.Header().Get("X-Gravlink-Patch-Source") != "embedded" {
		t.Fatalf("expected embedded patch fallback, got status=%d src=%q", patchRec.Code, patchRec.Header().Get("X-Gravlink-Patch-Source"))
	}

	// 6. Hot-reload override on disk takes precedence immediately without restart
	_ = os.WriteFile(filepath.Join(patchDir, "mobile-hub.css"), []byte("/* external hot reload */"), 0o644)
	extRec := httptest.NewRecorder()
	handler.ServeHTTP(extRec, patchReq)
	if extRec.Code != http.StatusOK || extRec.Header().Get("X-Gravlink-Patch-Source") != "external" || !strings.Contains(extRec.Body.String(), "external hot reload") {
		t.Fatalf("expected external hot-reload patch, got status=%d src=%q body=%s", extRec.Code, extRec.Header().Get("X-Gravlink-Patch-Source"), extRec.Body.String())
	}

	// 7. Arbitrary patch path rejected with 404
	badPatchReq := httptest.NewRequest(http.MethodGet, "/patches/evil.txt", nil)
	badPatchReq.RemoteAddr = "192.168.1.40:2222"
	badPatchRec := httptest.NewRecorder()
	handler.ServeHTTP(badPatchRec, badPatchReq)
	if badPatchRec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown patch asset, got %d", badPatchRec.Code)
	}

	// 8. Token usage report requires auth and returns JSON metrics
	unauthTok := httptest.NewRequest(http.MethodGet, "/api/tokens", nil)
	unauthTok.RemoteAddr = "192.168.1.40:2222"
	unauthTokRec := httptest.NewRecorder()
	handler.ServeHTTP(unauthTokRec, unauthTok)
	if unauthTokRec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on unauthenticated /api/tokens, got %d", unauthTokRec.Code)
	}

	tokReq := httptest.NewRequest(http.MethodGet, "/api/tokens", nil)
	tokReq.RemoteAddr = "192.168.1.40:2222"
	tokReq.AddCookie(cookie)
	tokRec := httptest.NewRecorder()
	handler.ServeHTTP(tokRec, tokReq)
	if tokRec.Code != http.StatusOK || !strings.Contains(tokRec.Body.String(), `"total_tokens"`) {
		t.Fatalf("expected token usage JSON, got %d (%s)", tokRec.Code, tokRec.Body.String())
	}
}
