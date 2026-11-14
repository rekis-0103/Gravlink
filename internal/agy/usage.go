package agy

import (
	"bufio"
	"encoding/json"
	"os"
	"sort"
	"strings"
	"time"
)

// SessionTokenUsage aggregates token counts and elapsed time for one conversation.
type SessionTokenUsage struct {
	ID              string    `json:"id"`
	Title           string    `json:"title"`
	LastStatus      string    `json:"last_status"`
	LastStepAt      time.Time `json:"last_step_at"`
	StepCount       int       `json:"step_count"`
	Turns           int       `json:"turns"`
	InputTokens     int64     `json:"input_tokens"`
	CacheReadTokens int64     `json:"cache_read_tokens"`
	OutputTokens    int64     `json:"output_tokens"`
	TotalTokens     int64     `json:"total_tokens"`
	DurationMs      int64     `json:"duration_ms"`
}

// TokenUsageReport holds global and per-session token usage metrics across Antigravity history.
type TokenUsageReport struct {
	TotalInputTokens     int64               `json:"total_input_tokens"`
	TotalCacheReadTokens int64               `json:"total_cache_read_tokens"`
	TotalOutputTokens    int64               `json:"total_output_tokens"`
	TotalTokens          int64               `json:"total_tokens"`
	TotalTurns           int                 `json:"total_turns"`
	TotalDurationMs      int64               `json:"total_duration_ms"`
	Sessions             []SessionTokenUsage `json:"sessions"`
}

// ComputeTokenUsage scans recent conversations in antigravityDir and aggregates token/timing statistics.
func ComputeTokenUsage(antigravityDir string, limit int) (*TokenUsageReport, error) {
	if limit <= 0 {
		limit = 30
	}
	convs, err := ListConversations(antigravityDir, limit)
	if err != nil {
		return nil, err
	}

	report := &TokenUsageReport{
		Sessions: make([]SessionTokenUsage, 0, len(convs)),
	}

	for _, c := range convs {
		tPath := TranscriptPath(antigravityDir, c.ID, false)
		su := summarizeTranscriptTokens(tPath, c)
		report.TotalInputTokens += su.InputTokens
		report.TotalCacheReadTokens += su.CacheReadTokens
		report.TotalOutputTokens += su.OutputTokens
		report.TotalTokens += su.TotalTokens
		report.TotalTurns += su.Turns
		report.TotalDurationMs += su.DurationMs
		report.Sessions = append(report.Sessions, su)
	}

	sort.Slice(report.Sessions, func(i, j int) bool {
		return report.Sessions[i].LastStepAt.After(report.Sessions[j].LastStepAt)
	})

	return report, nil
}

func summarizeTranscriptTokens(path string, base ConversationSummary) SessionTokenUsage {
	out := SessionTokenUsage{
		ID:         base.ID,
		Title:      base.Title,
		LastStatus: base.LastStatus,
		LastStepAt: base.LastStepAt,
		StepCount:  base.StepCount,
	}

	f, err := os.Open(path)
	if err != nil {
		return out
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 8*1024*1024)

	var currentTurnStart time.Time
	var hasTurnStart bool
	var lastTurnActivity time.Time

	flushTurn := func() {
		if hasTurnStart && !lastTurnActivity.IsZero() && lastTurnActivity.After(currentTurnStart) {
			out.DurationMs += lastTurnActivity.Sub(currentTurnStart).Milliseconds()
		}
		hasTurnStart = false
	}

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var step struct {
			Type            string `json:"type"`
			Status          string `json:"status"`
			CreatedAt       string `json:"created_at"`
			InputTokens     int64  `json:"input_tokens"`
			CacheReadTokens int64  `json:"cache_read_tokens"`
			OutputTokens    int64  `json:"output_tokens"`
		}
		if err := json.Unmarshal([]byte(line), &step); err != nil {
			continue
		}

		out.InputTokens += step.InputTokens
		out.CacheReadTokens += step.CacheReadTokens
		out.OutputTokens += step.OutputTokens

		ts, tsErr := time.Parse(time.RFC3339Nano, step.CreatedAt)
		if step.Type == "USER_INPUT" {
			flushTurn()
			out.Turns++
			if tsErr == nil {
				currentTurnStart = ts
				lastTurnActivity = ts
				hasTurnStart = true
			}
		} else if tsErr == nil && hasTurnStart {
			if ts.After(lastTurnActivity) {
				lastTurnActivity = ts
			}
		}
	}
	flushTurn()

	out.TotalTokens = out.InputTokens + out.CacheReadTokens + out.OutputTokens
	return out
}
