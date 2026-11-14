package agy

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// ToolCall represents an agent tool invocation inside a transcript step.
type ToolCall struct {
	Name string          `json:"name"`
	Args json.RawMessage `json:"args,omitempty"`
}

// TranscriptStep models a single line in Antigravity's transcript.jsonl with tolerant decoding.
type TranscriptStep struct {
	StepIndex       int        `json:"step_index"`
	Source          string     `json:"source"`
	Type            string     `json:"type"`
	Status          string     `json:"status"`
	CreatedAt       string     `json:"created_at"`
	Content         string     `json:"content,omitempty"`
	Thinking        string     `json:"thinking,omitempty"`
	ToolCalls       []ToolCall `json:"tool_calls,omitempty"`
	TruncatedFields []string   `json:"truncated_fields,omitempty"`
	InputTokens     int        `json:"input_tokens,omitempty"`
	CacheReadTokens int        `json:"cache_read_tokens,omitempty"`
	OutputTokens    int        `json:"output_tokens,omitempty"`
	Error           string     `json:"error,omitempty"`
}

// TranscriptPath returns the canonical path to transcript.jsonl or transcript_full.jsonl.
func TranscriptPath(antigravityDir, convID string, full bool) string {
	name := "transcript.jsonl"
	if full {
		name = "transcript_full.jsonl"
	}
	return filepath.Join(antigravityDir, "brain", filepath.Base(convID), ".system_generated", "logs", name)
}

// IsValidConversationID guards against path traversal in conversation IDs.
func IsValidConversationID(convID string) bool {
	if convID == "" || strings.Contains(convID, "..") || strings.ContainsAny(convID, `/\`) {
		return false
	}
	return true
}

// ReadTranscriptSteps reads steps with step_index >= fromIndex, automatically hydrating truncated fields from transcript_full.jsonl when present.
func ReadTranscriptSteps(path string, fromIndex int) ([]TranscriptStep, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	steps := make([]TranscriptStep, 0, 32)
	truncatedIndices := make(map[int]struct{})

	scanner := bufio.NewScanner(f)
	// Allow up to 8MB per JSONL line for large tool outputs.
	scanner.Buffer(make([]byte, 64*1024), 8*1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var step TranscriptStep
		if err := json.Unmarshal([]byte(line), &step); err != nil {
			// Tolerate partially flushed lines at EOF or future schema anomalies
			continue
		}
		if step.StepIndex < fromIndex {
			continue
		}
		if len(step.TruncatedFields) > 0 {
			truncatedIndices[step.StepIndex] = struct{}{}
		}
		steps = append(steps, step)
	}
	if err := scanner.Err(); err != nil {
		return steps, err
	}

	if len(truncatedIndices) > 0 && strings.HasSuffix(path, "transcript.jsonl") {
		fullPath := strings.TrimSuffix(path, "transcript.jsonl") + "transcript_full.jsonl"
		hydrateFromFull(fullPath, steps, truncatedIndices)
	}
	return steps, nil
}

func hydrateFromFull(fullPath string, steps []TranscriptStep, needed map[int]struct{}) {
	f, err := os.Open(fullPath)
	if err != nil {
		return
	}
	defer f.Close()

	fullByIndex := make(map[int]TranscriptStep, len(needed))
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var step TranscriptStep
		if err := json.Unmarshal([]byte(line), &step); err != nil {
			continue
		}
		if _, ok := needed[step.StepIndex]; ok {
			fullByIndex[step.StepIndex] = step
		}
	}

	for i := range steps {
		if fullStep, ok := fullByIndex[steps[i].StepIndex]; ok {
			// Preserve clean UI readability while restoring full truncated fields (cap huge logs at 256KB per step).
			if fullStep.Content != "" {
				steps[i].Content = capText(fullStep.Content, 256*1024)
			}
			if fullStep.Thinking != "" {
				steps[i].Thinking = capText(fullStep.Thinking, 128*1024)
			}
			if len(fullStep.ToolCalls) > 0 {
				steps[i].ToolCalls = fullStep.ToolCalls
			}
			steps[i].TruncatedFields = nil
		}
	}
}

func capText(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	return s[:maxBytes] + "\n\n... [truncated by Gravlink viewer]"
}

// ValidateTranscriptContract verifies a transcript file can be parsed and has expected core fields.
func ValidateTranscriptContract(path string) error {
	steps, err := ReadTranscriptSteps(path, 0)
	if err != nil {
		return err
	}
	if len(steps) == 0 {
		return errors.New("transcript has zero readable steps")
	}
	if steps[0].Type == "" || steps[0].Status == "" {
		return errors.New("transcript step missing required type/status fields")
	}
	return nil
}
