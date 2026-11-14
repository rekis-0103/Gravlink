package security

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// AuditEvent represents a single structured security or access log record.
type AuditEvent struct {
	Timestamp string `json:"ts"`
	Event     string `json:"event"`
	ClientIP  string `json:"client_ip"`
	Method    string `json:"method,omitempty"`
	Path      string `json:"path,omitempty"`
	Outcome   string `json:"outcome"`
	Detail    string `json:"detail,omitempty"`
}

// AuditLogger writes structured JSON-lines events to disk safely across goroutines.
type AuditLogger struct {
	mu   sync.Mutex
	file *os.File
}

// NewAuditLogger opens or creates audit.log in dataDir.
func NewAuditLogger(dataDir string) (*AuditLogger, error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(dataDir, "audit.log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	return &AuditLogger{file: f}, nil
}

// Log records an audit event synchronously.
func (l *AuditLogger) Log(ev AuditEvent) {
	if l == nil || l.file == nil {
		return
	}
	if ev.Timestamp == "" {
		ev.Timestamp = time.Now().UTC().Format(time.RFC3339Nano)
	}
	line, err := json.Marshal(ev)
	if err != nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	_, _ = l.file.Write(append(line, '\n'))
}

// Close closes the underlying log file.
func (l *AuditLogger) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.file.Close()
}
