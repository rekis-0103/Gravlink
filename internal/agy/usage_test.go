package agy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestComputeTokenUsage(t *testing.T) {
	agDir := t.TempDir()
	logDir := filepath.Join(agDir, "brain", "conv-tok-1", ".system_generated", "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatal(err)
	}

	jsonl := `{"step_index":0,"source":"USER_EXPLICIT","type":"USER_INPUT","status":"DONE","created_at":"2026-11-14T08:00:00Z","content":"hi"}` + "\n" +
		`{"step_index":1,"source":"MODEL","type":"PLANNER_RESPONSE","status":"DONE","created_at":"2026-11-14T08:00:05Z","input_tokens":1000,"cache_read_tokens":2500,"output_tokens":300,"content":"ok"}` + "\n" +
		`{"step_index":2,"source":"MODEL","type":"PLANNER_RESPONSE","status":"DONE","created_at":"2026-11-14T08:00:12Z","input_tokens":500,"cache_read_tokens":1500,"output_tokens":200,"content":"done"}` + "\n"

	if err := os.WriteFile(filepath.Join(logDir, "transcript.jsonl"), []byte(jsonl), 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := ComputeTokenUsage(agDir, 10)
	if err != nil {
		t.Fatalf("ComputeTokenUsage failed: %v", err)
	}
	if report.TotalInputTokens != 1500 || report.TotalCacheReadTokens != 4000 || report.TotalOutputTokens != 500 || report.TotalTokens != 6000 {
		t.Fatalf("unexpected token totals: %+v", report)
	}
	if report.TotalTurns != 1 || report.TotalDurationMs != 12000 {
		t.Fatalf("unexpected turns/duration: turns=%d dur=%d", report.TotalTurns, report.TotalDurationMs)
	}
	if len(report.Sessions) != 1 || report.Sessions[0].TotalTokens != 6000 {
		t.Fatalf("unexpected session breakdown: %+v", report.Sessions)
	}
}
