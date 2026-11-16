package agy

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestTranscriptReadAndHydrateFull(t *testing.T) {
	dir := t.TempDir()
	convID := "conv-1234"
	logsDir := filepath.Join(dir, "brain", convID, ".system_generated", "logs")
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	compact := `{"step_index":0,"source":"USER_EXPLICIT","type":"USER_INPUT","status":"DONE","created_at":"2026-11-14T05:38:43Z","content":"<USER_REQUEST>Build Gravlink</USER_REQUEST>"}
{"step_index":1,"source":"MODEL","type":"PLANNER_RESPONSE","status":"DONE","created_at":"2026-11-14T05:38:45Z","content":"short...","truncated_fields":["content"],"unknown_future_field":123}
`
	full := `{"step_index":0,"source":"USER_EXPLICIT","type":"USER_INPUT","status":"DONE","created_at":"2026-11-14T05:38:43Z","content":"<USER_REQUEST>Build Gravlink</USER_REQUEST>"}
{"step_index":1,"source":"MODEL","type":"PLANNER_RESPONSE","status":"DONE","created_at":"2026-11-14T05:38:45Z","content":"full detailed response from transcript_full"}
`
	if err := os.WriteFile(filepath.Join(logsDir, "transcript.jsonl"), []byte(compact), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(logsDir, "transcript_full.jsonl"), []byte(full), 0o644); err != nil {
		t.Fatal(err)
	}

	convs, err := ListConversations(dir, 10)
	if err != nil || len(convs) != 1 {
		t.Fatalf("expected 1 conversation, got len=%d err=%v", len(convs), err)
	}
	if convs[0].Title != "Build Gravlink" || !convs[0].HasFullLog {
		t.Fatalf("unexpected summary: %+v", convs[0])
	}

	steps, err := ReadTranscriptSteps(TranscriptPath(dir, convID, false), 0)
	if err != nil || len(steps) != 2 {
		t.Fatalf("expected 2 steps, got %d err=%v", len(steps), err)
	}
	if steps[1].Content != "full detailed response from transcript_full" {
		t.Fatalf("expected hydrated content, got %q", steps[1].Content)
	}
}

func TestDiscoverHubs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ok"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	_, portStr, _ := net.SplitHostPort(srv.Listener.Addr().String())
	port, _ := strconv.Atoi(portStr)

	dir := t.TempDir()
	daemonDir := filepath.Join(dir, "daemon")
	if err := os.MkdirAll(daemonDir, 0o755); err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(HubInfo{
		PID:       os.Getpid(),
		HTTPPort:  port,
		HTTPSPort: port + 2,
		LSVersion: "2.31.2",
		CSRFToken: "test-csrf",
	})
	if err := os.WriteFile(filepath.Join(daemonDir, "hub_"+portStr+".json"), payload, 0o644); err != nil {
		t.Fatal(err)
	}

	hubs, err := DiscoverHubs(context.Background(), dir)
	if err != nil || len(hubs) != 1 {
		t.Fatalf("expected 1 live hub, got %d err=%v", len(hubs), err)
	}
	if hubs[0].LSVersion != "2.31.2" {
		t.Fatalf("unexpected version: %s", hubs[0].LSVersion)
	}
}

func TestIsProcessAlive(t *testing.T) {
	if !isProcessAlive(os.Getpid()) {
		t.Fatalf("expected current PID %d to be alive", os.Getpid())
	}
	if isProcessAlive(0) || isProcessAlive(-5) {
		t.Fatalf("expected non-positive PID to be dead")
	}
	if isProcessAlive(99999999) {
		t.Fatalf("expected non-existent PID 99999999 to be dead")
	}
}
