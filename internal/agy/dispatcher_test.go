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

func TestDispatcherHubRPC(t *testing.T) {
	var gotCSRF string
	var gotPrompt string
	var gotInteract bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			w.WriteHeader(http.StatusOK)
		case "/exa.language_server_pb.LanguageServerService/GetAllCascadeTrajectories":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"trajectorySummaries": map[string]any{
					"conv-1": map[string]any{
						"summary":      "Test Session",
						"stepCount":    2,
						"trajectoryId": "traj-1",
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
							"sourceTrajectoryStepInfo": map[string]any{"trajectoryId": "traj-1"},
							"toolCall":                 map[string]any{"name": "run_command"},
							"requestedInteraction":     map[string]any{"runCommand": map[string]any{}},
						},
						"runCommand": map[string]any{"commandLine": "npm test"},
					},
				},
			})
		case "/exa.language_server_pb.LanguageServerService/SendUserCascadeMessage":
			gotCSRF = r.Header.Get("x-codeium-csrf-token")
			var body struct {
				CascadeID string              `json:"cascadeId"`
				Items     []map[string]string `json:"items"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if len(body.Items) > 0 {
				gotPrompt = body.Items[0]["text"]
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		case "/exa.language_server_pb.LanguageServerService/HandleCascadeUserInteraction":
			gotInteract = true
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	_, portStr, _ := net.SplitHostPort(srv.Listener.Addr().String())
	port, _ := strconv.Atoi(portStr)

	agDir := t.TempDir()
	daemonDir := filepath.Join(agDir, "daemon")
	_ = os.MkdirAll(daemonDir, 0o755)
	hubJSON, _ := json.Marshal(HubInfo{
		PID:       os.Getpid(),
		HTTPPort:  port,
		LSVersion: "2.31.2",
		CSRFToken: "hub-secret-csrf",
	})
	_ = os.WriteFile(filepath.Join(daemonDir, "hub_"+portStr+".json"), hubJSON, 0o644)

	disp := NewDispatcher(agDir)
	convID, via, err := disp.SendPrompt(context.Background(), PromptRequest{
		ConversationID: "conv-1",
		Prompt:         "Run tests please",
	})
	if err != nil || convID != "conv-1" || via != "hub_rpc" {
		t.Fatalf("SendPrompt failed: conv=%q via=%q err=%v", convID, via, err)
	}
	if gotCSRF != "hub-secret-csrf" || gotPrompt != "Run tests please" {
		t.Fatalf("unexpected RPC call: csrf=%q prompt=%q", gotCSRF, gotPrompt)
	}

	pending, status, err := disp.GetPendingInteractions(context.Background(), "conv-1")
	if err != nil || status != "CASCADE_RUN_STATUS_WAITING_FOR_USER" || len(pending) != 1 {
		t.Fatalf("GetPendingInteractions failed: pending=%+v status=%q err=%v", pending, status, err)
	}
	if pending[0].CommandLine != "npm test" {
		t.Fatalf("expected commandLine 'npm test', got %q", pending[0].CommandLine)
	}

	err = disp.RespondInteraction(context.Background(), "conv-1", InteractionRequest{
		TrajectoryID: pending[0].TrajectoryID,
		StepIndex:    pending[0].StepIndex,
		Kind:         pending[0].Kind,
		Allow:        true,
		CommandLine:  pending[0].CommandLine,
	})
	if err != nil || !gotInteract {
		t.Fatalf("RespondInteraction failed: err=%v gotInteract=%v", err, gotInteract)
	}
}
