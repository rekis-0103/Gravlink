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

func TestDispatcherHubRPCForwardsModeAndModel(t *testing.T) {
	var capturedPayload struct {
		CascadeID     string `json:"cascadeId"`
		CascadeConfig *struct {
			PlannerConfig *struct {
				RequestedModel map[string]string `json:"requestedModel"`
				Conversational *struct {
					PlannerMode string `json:"plannerMode"`
					AgenticMode bool   `json:"agenticMode"`
				} `json:"conversational"`
			} `json:"plannerConfig"`
		} `json:"cascadeConfig"`
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			w.WriteHeader(http.StatusOK)
		case "/exa.language_server_pb.LanguageServerService/GetCascadeModelConfigData":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"clientModelConfigs": []map[string]any{
					{
						"label":        "Gemini 4 Argon (Medium)",
						"modelId":      "gemini-4-argon-medium",
						"modelOrAlias": map[string]string{"model": "MODEL_PLACEHOLDER_M418"},
					},
					{
						"label":        "Gemini 4 Carbon",
						"modelId":      "gemini-4-carbon-disabled",
						"modelOrAlias": map[string]string{"model": "MODEL_PLACEHOLDER_M670"},
					},
				},
			})
		case "/exa.language_server_pb.LanguageServerService/SendUserCascadeMessage":
			_ = json.NewDecoder(r.Body).Decode(&capturedPayload)
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

	models := disp.ListModels(context.Background())
	if len(models) != 1 || models[0].ID != "gemini-4-argon-medium" {
		t.Fatalf("expected 1 active model from Hub catalog, got %+v", models)
	}

	_, via, err := disp.SendPrompt(context.Background(), PromptRequest{
		ConversationID: "conv-plan",
		Prompt:         "Draft architecture plan",
		Mode:           "plan",
		Model:          "gemini-4-argon-medium",
	})
	if err != nil || via != "hub_rpc" {
		t.Fatalf("SendPrompt(plan) failed: via=%q err=%v", via, err)
	}
	if capturedPayload.CascadeConfig == nil || capturedPayload.CascadeConfig.PlannerConfig == nil {
		t.Fatalf("expected cascadeConfig.plannerConfig in RPC payload, got nil")
	}
	pc := capturedPayload.CascadeConfig.PlannerConfig
	if pc.RequestedModel["model"] != "MODEL_PLACEHOLDER_M418" {
		t.Fatalf("expected MODEL_PLACEHOLDER_M418, got %+v", pc.RequestedModel)
	}
	if pc.Conversational == nil || pc.Conversational.PlannerMode != "CONVERSATIONAL_PLANNER_MODE_READ_ONLY" {
		t.Fatalf("expected CONVERSATIONAL_PLANNER_MODE_READ_ONLY for plan mode, got %+v", pc.Conversational)
	}

	_, via, err = disp.SendPrompt(context.Background(), PromptRequest{
		ConversationID: "conv-auto",
		Prompt:         "Apply edit",
		Mode:           "auto",
		Model:          "auto",
	})
	if err != nil || via != "hub_rpc" {
		t.Fatalf("SendPrompt(auto) failed: via=%q err=%v", via, err)
	}
	pc = capturedPayload.CascadeConfig.PlannerConfig
	if pc.RequestedModel["alias"] != "MODEL_ALIAS_AUTO" {
		t.Fatalf("expected MODEL_ALIAS_AUTO, got %+v", pc.RequestedModel)
	}
	if pc.Conversational == nil || pc.Conversational.PlannerMode != "CONVERSATIONAL_PLANNER_MODE_DEFAULT" || !pc.Conversational.AgenticMode {
		t.Fatalf("expected CONVERSATIONAL_PLANNER_MODE_DEFAULT with agenticMode=true, got %+v", pc.Conversational)
	}
}

func TestDispatcherLimitedBufferAndErrorLogger(t *testing.T) {
	lb := &limitedBuffer{max: 10}
	n, err := lb.Write([]byte("1234567890abcdef"))
	if err != nil || n != 16 {
		t.Fatalf("unexpected write result: n=%d err=%v", n, err)
	}
	if lb.buf.String() != "1234567890" {
		t.Fatalf("expected capped output '1234567890', got %q", lb.buf.String())
	}

	disp := NewDispatcher(t.TempDir())
	var gotEvent, gotDetail string
	disp.SetErrorLogger(func(event, detail string) {
		gotEvent = event
		gotDetail = detail
	})
	disp.logAsyncError("cli_fallback_failed", "exit status 1 stderr=boom")
	if gotEvent != "cli_fallback_failed" || gotDetail != "exit status 1 stderr=boom" {
		t.Fatalf("unexpected error logger capture: event=%q detail=%q", gotEvent, gotDetail)
	}
}
