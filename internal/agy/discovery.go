package agy

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// HubInfo mirrors ~/.gemini/antigravity/daemon/hub_<port>.json.
type HubInfo struct {
	PID       int    `json:"pid"`
	HTTPSPort int    `json:"httpsPort"`
	HTTPPort  int    `json:"httpPort"`
	LSVersion string `json:"lsVersion"`
	CSRFToken string `json:"csrfToken"`
	FilePath  string `json:"filePath"`
	Healthy   bool   `json:"healthy"`
}

// ConversationSummary describes a discovered Antigravity conversation in brain/.
type ConversationSummary struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	LastStatus  string    `json:"last_status"`
	LastStepAt  time.Time `json:"last_step_at"`
	StepCount   int       `json:"step_count"`
	HasFullLog  bool      `json:"has_full_log"`
	Workspace   string    `json:"workspace,omitempty"`
}

// DiscoverHubs scans <antigravityDir>/daemon/hub_*.json and probes /healthz on localhost.
func DiscoverHubs(ctx context.Context, antigravityDir string) ([]HubInfo, error) {
	daemonDir := filepath.Join(antigravityDir, "daemon")
	entries, err := os.ReadDir(daemonDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	client := &http.Client{Timeout: 800 * time.Millisecond}
	var hubs []HubInfo
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "hub_") || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		fullPath := filepath.Join(daemonDir, entry.Name())
		raw, err := os.ReadFile(fullPath)
		if err != nil {
			continue
		}
		var info HubInfo
		if err := json.Unmarshal(raw, &info); err != nil {
			continue
		}
		info.FilePath = fullPath
		if info.HTTPPort > 0 && isProcessAlive(info.PID) && probeHealth(ctx, client, info.HTTPPort) {
			info.Healthy = true
			hubs = append(hubs, info)
		}
	}

	sort.Slice(hubs, func(i, j int) bool {
		return hubs[i].HTTPPort > hubs[j].HTTPPort
	})
	return hubs, nil
}

func probeHealth(ctx context.Context, client *http.Client, port int) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:"+strconv.Itoa(port)+"/healthz", nil)
	if err != nil {
		return false
	}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 500
}

func isProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil || proc == nil {
		return false
	}
	return true
}

// FindAgyBinary attempts to locate agy.exe / agy on the system.
func FindAgyBinary() string {
	home, err := os.UserHomeDir()
	if err == nil {
		candidate := filepath.Join(home, ".gemini", "bin", "agy.exe")
		if _, statErr := os.Stat(candidate); statErr == nil {
			return candidate
		}
		candidateUnix := filepath.Join(home, ".gemini", "bin", "agy")
		if _, statErr := os.Stat(candidateUnix); statErr == nil {
			return candidateUnix
		}
	}
	if p, err := exec.LookPath("agy"); err == nil {
		return p
	}
	return ""
}

// ListConversations enumerates conversations from <antigravityDir>/brain/ ordered by latest activity.
func ListConversations(antigravityDir string, limit int) ([]ConversationSummary, error) {
	if limit <= 0 {
		limit = 30
	}
	brainDir := filepath.Join(antigravityDir, "brain")
	entries, err := os.ReadDir(brainDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var results []ConversationSummary
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == "tempmediaStorage" {
			continue
		}
		convID := entry.Name()
		transcriptPath := TranscriptPath(antigravityDir, convID, false)
		info, err := os.Stat(transcriptPath)
		if err != nil {
			continue
		}
		steps, err := ReadTranscriptSteps(transcriptPath, 0)
		if err != nil || len(steps) == 0 {
			continue
		}

		summary := ConversationSummary{
			ID:         convID,
			Title:      extractConversationTitle(convID, steps),
			LastStatus: steps[len(steps)-1].Status,
			LastStepAt: info.ModTime().UTC(),
			StepCount:  len(steps),
		}
		if _, err := os.Stat(TranscriptPath(antigravityDir, convID, true)); err == nil {
			summary.HasFullLog = true
		}
		if t, err := time.Parse(time.RFC3339Nano, steps[len(steps)-1].CreatedAt); err == nil {
			summary.LastStepAt = t.UTC()
		}
		results = append(results, summary)
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].LastStepAt.After(results[j].LastStepAt)
	})
	if len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

func extractConversationTitle(convID string, steps []TranscriptStep) string {
	for _, s := range steps {
		if s.Type == "USER_INPUT" && strings.TrimSpace(s.Content) != "" {
			clean := cleanUserPromptPreview(s.Content)
			if clean != "" {
				if len(clean) > 80 {
					return clean[:80] + "..."
				}
				return clean
			}
		}
	}
	if len(convID) > 12 {
		return "Session " + convID[:8]
	}
	return "Session " + convID
}

func cleanUserPromptPreview(raw string) string {
	text := raw
	if start := strings.Index(text, "<USER_REQUEST>"); start != -1 {
		after := text[start+len("<USER_REQUEST>"):]
		if end := strings.Index(after, "</USER_REQUEST>"); end != -1 {
			text = after[:end]
		} else {
			text = after
		}
	}
	text = strings.TrimSpace(text)
	text = strings.ReplaceAll(text, "\r\n", " ")
	text = strings.ReplaceAll(text, "\n", " ")
	return strings.TrimSpace(strings.Join(strings.Fields(text), " "))
}
