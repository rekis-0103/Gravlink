package agy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// TrajectorySummary mirrors key fields from GetAllCascadeTrajectories.
type TrajectorySummary struct {
	Summary          string `json:"summary"`
	StepCount        int    `json:"stepCount"`
	LastModifiedTime string `json:"lastModifiedTime"`
	TrajectoryID     string `json:"trajectoryId"`
	Status           string `json:"status"`
}

// PendingInteraction represents an active tool/command confirmation waiting for the user.
type PendingInteraction struct {
	CascadeID    string `json:"cascade_id"`
	TrajectoryID string `json:"trajectory_id"`
	StepIndex    int    `json:"step_index"`
	Kind         string `json:"kind"` // runCommand, permission, confirm, unknown
	ToolName     string `json:"tool_name,omitempty"`
	CommandLine  string `json:"command_line,omitempty"`
	Resource     string `json:"resource,omitempty"`
	Prompt       string `json:"prompt,omitempty"`
}

// ModelOption represents an available Antigravity model.
type ModelOption struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type modelCatalogEntry struct {
	ID           string
	Label        string
	ModelOrAlias map[string]string
}

type limitedBuffer struct {
	buf bytes.Buffer
	max int
}

func (l *limitedBuffer) Write(p []byte) (int, error) {
	if l.max > 0 && l.buf.Len() < l.max {
		remain := l.max - l.buf.Len()
		if len(p) > remain {
			_, _ = l.buf.Write(p[:remain])
		} else {
			_, _ = l.buf.Write(p)
		}
	}
	return len(p), nil
}

// PromptRequest configures a prompt dispatched from mobile to Antigravity.
type PromptRequest struct {
	ConversationID string `json:"conversation_id"`
	Prompt         string `json:"prompt"`
	Mode           string `json:"mode,omitempty"`  // auto, plan, accept-edits
	Model          string `json:"model,omitempty"` // optional model ID
	Workspace      string `json:"workspace,omitempty"`
}

// InteractionRequest approves or rejects a pending user interaction.
type InteractionRequest struct {
	TrajectoryID string `json:"trajectory_id"`
	StepIndex    int    `json:"step_index"`
	Kind         string `json:"kind"`
	Allow        bool   `json:"allow"`
	CommandLine  string `json:"command_line,omitempty"`
}

// Dispatcher sends prompts and tool approvals to Antigravity via Hub Connect-RPC or CLI fallback.
type Dispatcher struct {
	antigravityDir string
	httpClient     *http.Client

	modelsMu      sync.Mutex
	cachedAt      time.Time
	cachedModel   []ModelOption
	cachedCatalog []modelCatalogEntry

	errLogMu sync.RWMutex
	errLogFn func(event, detail string)
}

// NewDispatcher creates a Dispatcher bound to antigravityDir.
func NewDispatcher(antigravityDir string) *Dispatcher {
	return &Dispatcher{
		antigravityDir: antigravityDir,
		httpClient:     &http.Client{Timeout: 10 * time.Second},
	}
}

// SetErrorLogger registers a callback to record asynchronous dispatch failures.
func (d *Dispatcher) SetErrorLogger(fn func(event, detail string)) {
	d.errLogMu.Lock()
	d.errLogFn = fn
	d.errLogMu.Unlock()
}

func (d *Dispatcher) logAsyncError(event, detail string) {
	log.Printf("[gravlink/agy] %s: %s", event, detail)
	d.errLogMu.RLock()
	fn := d.errLogFn
	d.errLogMu.RUnlock()
	if fn != nil {
		fn(event, detail)
	}
}

func (d *Dispatcher) callHubRPC(ctx context.Context, hub HubInfo, method string, reqBody any, respOut any) error {
	raw, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}
	endpoint := fmt.Sprintf("http://127.0.0.1:%d/exa.language_server_pb.LanguageServerService/%s", hub.HTTPPort, method)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if hub.CSRFToken != "" {
		req.Header.Set("x-codeium-csrf-token", hub.CSRFToken)
	}

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("hub RPC %s returned HTTP %d: %s", method, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if respOut != nil && len(body) > 0 {
		return json.Unmarshal(body, respOut)
	}
	return nil
}

// GetAllTrajectories fetches live trajectory summaries from the active Hub, if running.
func (d *Dispatcher) GetAllTrajectories(ctx context.Context) (map[string]TrajectorySummary, error) {
	hubs, err := DiscoverHubs(ctx, d.antigravityDir)
	if err != nil || len(hubs) == 0 {
		return nil, err
	}
	var res struct {
		TrajectorySummaries map[string]TrajectorySummary `json:"trajectorySummaries"`
	}
	if err := d.callHubRPC(ctx, hubs[0], "GetAllCascadeTrajectories", map[string]any{}, &res); err != nil {
		return nil, err
	}
	return res.TrajectorySummaries, nil
}

// GetPendingInteractions inspects the tail steps of a cascade for any pending approval/input.
func (d *Dispatcher) GetPendingInteractions(ctx context.Context, cascadeID string) ([]PendingInteraction, string, error) {
	if !IsValidConversationID(cascadeID) {
		return nil, "", errors.New("invalid conversation id")
	}
	hubs, err := DiscoverHubs(ctx, d.antigravityDir)
	if err != nil || len(hubs) == 0 {
		return nil, "", nil
	}
	hub := hubs[0]

	var trajStatus string
	var stepCount int
	summaries, err := d.GetAllTrajectories(ctx)
	if err == nil {
		if s, ok := summaries[cascadeID]; ok {
			trajStatus = s.Status
			stepCount = s.StepCount
		}
	}

	offset := 0
	if stepCount > 25 {
		offset = stepCount - 25
	}

	var stepsResp struct {
		Steps []map[string]any `json:"steps"`
	}
	if err := d.callHubRPC(ctx, hub, "GetCascadeTrajectorySteps", map[string]any{
		"cascadeId":  cascadeID,
		"stepOffset": offset,
	}, &stepsResp); err != nil {
		return nil, trajStatus, err
	}

	var pending []PendingInteraction
	for idx, rawStep := range stepsResp.Steps {
		status, _ := rawStep["status"].(string)
		meta, _ := rawStep["metadata"].(map[string]any)
		reqInter, hasInter := meta["requestedInteraction"].(map[string]any)
		if status != "CORTEX_STEP_STATUS_WAITING" && !hasInter {
			continue
		}

		trajID := ""
		if srcInfo, ok := meta["sourceTrajectoryStepInfo"].(map[string]any); ok {
			trajID, _ = srcInfo["trajectoryId"].(string)
		}

		item := PendingInteraction{
			CascadeID:    cascadeID,
			TrajectoryID: trajID,
			StepIndex:    offset + idx,
			Kind:         "unknown",
		}
		if tc, ok := meta["toolCall"].(map[string]any); ok {
			item.ToolName, _ = tc["name"].(string)
		}

		if runCmd, ok := rawStep["runCommand"].(map[string]any); ok {
			item.Kind = "runCommand"
			item.CommandLine, _ = runCmd["commandLine"].(string)
			if item.CommandLine == "" {
				item.CommandLine, _ = runCmd["command"].(string)
			}
		}
		if hasInter {
			if perm, ok := reqInter["permission"].(map[string]any); ok {
				item.Kind = "permission"
				if resMap, ok := perm["resource"].(map[string]any); ok {
					item.Resource, _ = resMap["target"].(string)
				}
			} else if _, ok := reqInter["runCommand"].(map[string]any); ok {
				item.Kind = "runCommand"
			} else if _, ok := reqInter["confirm"].(map[string]any); ok {
				item.Kind = "confirm"
			}
		}
		pending = append(pending, item)
	}

	return pending, trajStatus, nil
}

// SendPrompt dispatches a prompt to an active Hub cascade or falls back to headless agy CLI.
func (d *Dispatcher) SendPrompt(ctx context.Context, req PromptRequest) (string, string, error) {
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		return "", "", errors.New("prompt cannot be empty")
	}

	hubs, _ := DiscoverHubs(ctx, d.antigravityDir)
	if len(hubs) > 0 {
		hub := hubs[0]
		convID := strings.TrimSpace(req.ConversationID)
		if convID == "" || convID == "new" {
			var startRes struct {
				CascadeID string `json:"cascadeId"`
			}
			if err := d.callHubRPC(ctx, hub, "StartCascade", map[string]any{}, &startRes); err == nil && startRes.CascadeID != "" {
				convID = startRes.CascadeID
			}
		}
		if convID != "" && IsValidConversationID(convID) {
			rpcPayload := map[string]any{
				"cascadeId": convID,
				"items": []map[string]any{
					{"text": prompt},
				},
			}
			if cascadeCfg := d.buildCascadeConfig(ctx, hub, req); cascadeCfg != nil {
				rpcPayload["cascadeConfig"] = cascadeCfg
			}
			if err := d.callHubRPC(ctx, hub, "SendUserCascadeMessage", rpcPayload, nil); err == nil {
				return convID, "hub_rpc", nil
			} else {
				d.logAsyncError("hub_rpc_send_failed", fmt.Sprintf("conv=%s err=%v", convID, err))
			}
		}
	}

	// Fallback: invoke agy CLI asynchronously so transcript.jsonl streams live updates.
	agyBin := FindAgyBinary()
	if agyBin == "" {
		return "", "", errors.New("no live Antigravity Hub running and agy CLI binary not found")
	}

	args := []string{"-p", prompt, "--output-format", "stream-json"}
	if req.ConversationID != "" && req.ConversationID != "new" && IsValidConversationID(req.ConversationID) {
		args = append(args, "--conversation", req.ConversationID)
	}
	if req.Mode == "plan" || req.Mode == "auto" || req.Mode == "accept-edits" {
		args = append(args, "--mode", req.Mode)
	}
	if req.Model != "" {
		args = append(args, "--model", req.Model)
	}
	if req.Workspace != "" {
		args = append(args, "--add-dir", req.Workspace)
	}

	cmd := exec.Command(agyBin, args...)
	if req.Workspace != "" {
		cmd.Dir = req.Workspace
	}
	stderrBuf := &limitedBuffer{max: 8192}
	cmd.Stderr = stderrBuf
	if err := cmd.Start(); err != nil {
		return "", "", fmt.Errorf("failed starting agy CLI: %w", err)
	}
	go func(targetConv string) {
		if waitErr := cmd.Wait(); waitErr != nil {
			detail := fmt.Sprintf("conv=%s err=%v", targetConv, waitErr)
			if errText := strings.TrimSpace(stderrBuf.buf.String()); errText != "" {
				detail += " stderr=" + errText
			}
			d.logAsyncError("cli_fallback_failed", detail)
		}
	}(req.ConversationID)

	return req.ConversationID, "agy_cli", nil
}

func (d *Dispatcher) buildCascadeConfig(ctx context.Context, hub HubInfo, req PromptRequest) map[string]any {
	mode := strings.ToLower(strings.TrimSpace(req.Mode))
	model := strings.TrimSpace(req.Model)
	if mode == "" && model == "" {
		return nil
	}

	plannerConfig := map[string]any{}
	switch mode {
	case "plan":
		plannerConfig["conversational"] = map[string]any{
			"plannerMode": "CONVERSATIONAL_PLANNER_MODE_READ_ONLY",
		}
	case "auto", "accept-edits":
		plannerConfig["conversational"] = map[string]any{
			"plannerMode": "CONVERSATIONAL_PLANNER_MODE_DEFAULT",
			"agenticMode": true,
		}
	}

	if model != "" {
		if moa := d.resolveModelOrAlias(ctx, hub, model); len(moa) > 0 {
			plannerConfig["requestedModel"] = moa
		}
	}

	if len(plannerConfig) == 0 {
		return nil
	}
	return map[string]any{
		"plannerConfig": plannerConfig,
	}
}

func (d *Dispatcher) resolveModelOrAlias(ctx context.Context, hub HubInfo, rawModel string) map[string]string {
	model := strings.TrimSpace(rawModel)
	if model == "" {
		return nil
	}
	if strings.HasPrefix(model, "MODEL_ALIAS_") {
		return map[string]string{"alias": model}
	}
	if strings.HasPrefix(model, "MODEL_") {
		return map[string]string{"model": model}
	}
	if strings.EqualFold(model, "auto") {
		return map[string]string{"alias": "MODEL_ALIAS_AUTO"}
	}
	if strings.EqualFold(model, "recommended") {
		return map[string]string{"alias": "MODEL_ALIAS_RECOMMENDED"}
	}

	d.modelsMu.Lock()
	catalog := append([]modelCatalogEntry(nil), d.cachedCatalog...)
	fresh := len(catalog) > 0 && time.Since(d.cachedAt) < 10*time.Minute
	d.modelsMu.Unlock()

	if !fresh {
		if fetched := d.fetchHubModelCatalog(ctx, hub); len(fetched) > 0 {
			catalog = fetched
			d.modelsMu.Lock()
			d.cachedCatalog = fetched
			if len(d.cachedModel) == 0 {
				d.cachedModel = catalogToOptions(fetched)
			}
			d.cachedAt = time.Now()
			d.modelsMu.Unlock()
		}
	}

	for _, entry := range catalog {
		if strings.EqualFold(model, entry.ID) ||
			strings.EqualFold(model, entry.Label) ||
			strings.EqualFold(model, entry.ModelOrAlias["model"]) ||
			strings.EqualFold(model, entry.ModelOrAlias["alias"]) {
			if len(entry.ModelOrAlias) > 0 {
				copyMap := make(map[string]string, len(entry.ModelOrAlias))
				for k, v := range entry.ModelOrAlias {
					copyMap[k] = v
				}
				return copyMap
			}
		}
	}

	return map[string]string{"model": model}
}

func (d *Dispatcher) fetchHubModelCatalog(ctx context.Context, hub HubInfo) []modelCatalogEntry {
	subCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	var res struct {
		ClientModelConfigs []struct {
			Label        string            `json:"label"`
			ModelID      string            `json:"modelId"`
			ModelOrAlias map[string]string `json:"modelOrAlias"`
		} `json:"clientModelConfigs"`
	}
	if err := d.callHubRPC(subCtx, hub, "GetCascadeModelConfigData", map[string]any{}, &res); err != nil {
		return nil
	}
	var out []modelCatalogEntry
	for _, cfg := range res.ClientModelConfigs {
		id := strings.TrimSpace(cfg.ModelID)
		if id == "" {
			id = strings.TrimSpace(cfg.ModelOrAlias["model"])
		}
		if id == "" {
			id = strings.TrimSpace(cfg.ModelOrAlias["alias"])
		}
		if id == "" {
			continue
		}
		out = append(out, modelCatalogEntry{
			ID:           id,
			Label:        strings.TrimSpace(cfg.Label),
			ModelOrAlias: cfg.ModelOrAlias,
		})
	}
	return out
}

func catalogToOptions(catalog []modelCatalogEntry) []ModelOption {
	out := make([]ModelOption, 0, len(catalog))
	for _, e := range catalog {
		if strings.HasSuffix(e.ID, "-disabled") {
			continue
		}
		label := e.Label
		if label == "" {
			label = e.ID
		}
		out = append(out, ModelOption{ID: e.ID, Label: label})
	}
	return out
}

// RespondInteraction forwards an approval/rejection decision to the live Hub RPC.
func (d *Dispatcher) RespondInteraction(ctx context.Context, cascadeID string, req InteractionRequest) error {
	if !IsValidConversationID(cascadeID) {
		return errors.New("invalid conversation id")
	}
	hubs, err := DiscoverHubs(ctx, d.antigravityDir)
	if err != nil || len(hubs) == 0 {
		return errors.New("no live Antigravity Hub available for interactive approval")
	}
	hub := hubs[0]

	var interaction any
	switch req.Kind {
	case "permission":
		interaction = map[string]any{
			"permission": map[string]any{
				"allow": req.Allow,
				"scope": "PERMISSION_SCOPE_ONCE",
			},
		}
	case "runCommand":
		interaction = map[string]any{
			"runCommand": map[string]any{
				"confirm":             req.Allow,
				"proposedCommandLine": req.CommandLine,
			},
		}
	default:
		interaction = map[string]any{
			"confirm": map[string]any{
				"confirm": req.Allow,
			},
		}
	}

	payload := map[string]any{
		"cascadeId": cascadeID,
		"interaction": map[string]any{
			"trajectoryId": req.TrajectoryID,
			"stepIndex":    req.StepIndex,
		},
	}
	// Merge selected interaction oneof key into interaction object
	interMap := payload["interaction"].(map[string]any)
	for k, v := range interaction.(map[string]any) {
		interMap[k] = v
	}

	return d.callHubRPC(ctx, hub, "HandleCascadeUserInteraction", payload, nil)
}

// ListModels returns available models from a live Hub (preferred) or `agy models`, cached for 10 minutes.
func (d *Dispatcher) ListModels(ctx context.Context) []ModelOption {
	d.modelsMu.Lock()
	if len(d.cachedModel) > 0 && time.Since(d.cachedAt) < 10*time.Minute {
		out := append([]ModelOption(nil), d.cachedModel...)
		d.modelsMu.Unlock()
		return out
	}
	d.modelsMu.Unlock()

	if hubs, err := DiscoverHubs(ctx, d.antigravityDir); err == nil && len(hubs) > 0 {
		if catalog := d.fetchHubModelCatalog(ctx, hubs[0]); len(catalog) > 0 {
			if opts := catalogToOptions(catalog); len(opts) > 0 {
				d.modelsMu.Lock()
				d.cachedCatalog = catalog
				d.cachedModel = opts
				d.cachedAt = time.Now()
				d.modelsMu.Unlock()
				return opts
			}
		}
	}

	agyBin := FindAgyBinary()
	if agyBin == "" {
		return defaultModels()
	}

	runCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(runCtx, agyBin, "models").Output()
	if err != nil {
		return defaultModels()
	}

	var parsed []ModelOption
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "Fetching") {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) == 2 {
			parsed = append(parsed, ModelOption{
				ID:    strings.TrimSpace(parts[0]),
				Label: strings.TrimSpace(parts[1]),
			})
		}
	}
	if len(parsed) == 0 {
		return defaultModels()
	}

	d.modelsMu.Lock()
	d.cachedModel = parsed
	d.cachedAt = time.Now()
	d.modelsMu.Unlock()
	return parsed
}

func defaultModels() []ModelOption {
	return []ModelOption{
		{ID: "gemini-4-argon-medium", Label: "Gemini 4 Argon (Medium)"},
		{ID: "gemini-4-hydrogen-high", Label: "Gemini 4 Hydrogen (High)"},
		{ID: "gemini-3.8-flash-medium", Label: "Gemini 3.8 Flash (Medium)"},
	}
}

// HubURL returns the localhost HTTP URL for the first healthy hub.
func HubURL(h HubInfo) string {
	return "http://127.0.0.1:" + strconv.Itoa(h.HTTPPort)
}
