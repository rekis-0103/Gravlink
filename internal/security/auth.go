package security

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/argon2"
)

const (
	SessionCookieName = "gravlink_session"
	CSRFHeaderName    = "X-Gravlink-CSRF"

	argonTime    = uint32(3)
	argonMemory  = uint32(64 * 1024)
	argonThreads = uint8(4)
	argonKeyLen  = uint32(32)
)

// HashPassword generates an Argon2id PHC-encoded password hash.
func HashPassword(password string) (string, error) {
	if len(password) < 8 {
		return "", errors.New("password must be at least 8 characters")
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	hash := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		argonMemory,
		argonTime,
		argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash),
	), nil
}

// VerifyPassword verifies a plaintext password against a PHC-encoded Argon2id hash.
func VerifyPassword(password, encoded string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, errors.New("invalid argon2id hash format")
	}

	var memory uint32
	var iterations uint32
	var parallelism uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iterations, &parallelism); err != nil {
		return false, fmt.Errorf("invalid argon2id params: %w", err)
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, err
	}
	expectedHash, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, err
	}

	actualHash := argon2.IDKey([]byte(password), salt, iterations, memory, parallelism, uint32(len(expectedHash)))
	return subtle.ConstantTimeCompare(actualHash, expectedHash) == 1, nil
}

// SessionClaims represents authenticated session metadata embedded in the signed cookie.
type SessionClaims struct {
	SessionID string `json:"sid"`
	CSRFToken string `json:"csrf"`
	ClientIP  string `json:"ip"`
	ExpiresAt int64  `json:"exp"`
}

type pairingEntry struct {
	token     string
	expiresAt time.Time
}

// SessionManager signs/verifies session cookies and manages single-use QR pairing tokens.
type SessionManager struct {
	secret     []byte
	ttl        time.Duration
	secureHTTP bool
	mu         sync.Mutex
	pairing    *pairingEntry
}

// NewSessionManager constructs a SessionManager from a hex-encoded 32-byte secret.
func NewSessionManager(secretHex string, ttl time.Duration, secureTLS bool) (*SessionManager, error) {
	secret, err := hex.DecodeString(strings.TrimSpace(secretHex))
	if err != nil || len(secret) < 16 {
		return nil, errors.New("invalid session secret")
	}
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	return &SessionManager{
		secret:     secret,
		ttl:        ttl,
		secureHTTP: secureTLS,
	}, nil
}

// IssueSession creates a new session cookie and returns its claims.
func (sm *SessionManager) IssueSession(w http.ResponseWriter, clientIP string) (*SessionClaims, error) {
	sid, err := randomHex(16)
	if err != nil {
		return nil, err
	}
	csrf, err := randomHex(24)
	if err != nil {
		return nil, err
	}
	claims := &SessionClaims{
		SessionID: sid,
		CSRFToken: csrf,
		ClientIP:  clientIP,
		ExpiresAt: time.Now().Add(sm.ttl).Unix(),
	}
	raw, err := json.Marshal(claims)
	if err != nil {
		return nil, err
	}
	payload := base64.RawURLEncoding.EncodeToString(raw)
	sig := sm.sign(payload)
	token := payload + "." + sig

	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  time.Unix(claims.ExpiresAt, 0),
		MaxAge:   int(sm.ttl.Seconds()),
		HttpOnly: true,
		Secure:   sm.secureHTTP,
		SameSite: http.SameSiteStrictMode,
	})
	return claims, nil
}

// ClearSession removes the session cookie.
func (sm *SessionManager) ClearSession(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
		HttpOnly: true,
		Secure:   sm.secureHTTP,
		SameSite: http.SameSiteStrictMode,
	})
}

// VerifyRequest validates the session cookie on an HTTP request.
func (sm *SessionManager) VerifyRequest(r *http.Request) (*SessionClaims, bool) {
	cookie, err := r.Cookie(SessionCookieName)
	if err != nil || cookie.Value == "" {
		return nil, false
	}
	parts := strings.Split(cookie.Value, ".")
	if len(parts) != 2 {
		return nil, false
	}
	payload, sig := parts[0], parts[1]
	expectedSig := sm.sign(payload)
	if subtle.ConstantTimeCompare([]byte(sig), []byte(expectedSig)) != 1 {
		return nil, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return nil, false
	}
	var claims SessionClaims
	if err := json.Unmarshal(raw, &claims); err != nil {
		return nil, false
	}
	if time.Now().Unix() > claims.ExpiresAt || claims.SessionID == "" || claims.CSRFToken == "" {
		return nil, false
	}
	return &claims, true
}

// CreatePairingToken generates a short-lived, single-use token for QR code login.
func (sm *SessionManager) CreatePairingToken(ttl time.Duration) (string, error) {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	tok, err := randomHex(24)
	if err != nil {
		return "", err
	}
	sm.mu.Lock()
	sm.pairing = &pairingEntry{
		token:     tok,
		expiresAt: time.Now().Add(ttl),
	}
	sm.mu.Unlock()
	return tok, nil
}

// ConsumePairingToken verifies and invalidates the active pairing token in constant time.
func (sm *SessionManager) ConsumePairingToken(token string) bool {
	token = strings.TrimSpace(token)
	if token == "" {
		return false
	}
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if sm.pairing == nil {
		return false
	}
	if time.Now().After(sm.pairing.expiresAt) {
		sm.pairing = nil
		return false
	}
	match := subtle.ConstantTimeCompare([]byte(token), []byte(sm.pairing.token)) == 1
	if match {
		sm.pairing = nil
	}
	return match
}

// RequireAuth enforces valid session + same-origin + CSRF on state-changing requests.
func (sm *SessionManager) RequireAuth(audit *AuditLogger, next func(http.ResponseWriter, *http.Request, *SessionClaims)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !ValidateSameOrigin(r) {
			if audit != nil {
				audit.Log(AuditEvent{
					Event:    "origin_mismatch",
					ClientIP: r.RemoteAddr,
					Method:   r.Method,
					Path:     r.URL.Path,
					Outcome:  "denied",
					Detail:   r.Header.Get("Origin"),
				})
			}
			http.Error(w, "Forbidden: origin mismatch", http.StatusForbidden)
			return
		}

		claims, ok := sm.VerifyRequest(r)
		if !ok {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
			csrf := r.Header.Get(CSRFHeaderName)
			if subtle.ConstantTimeCompare([]byte(csrf), []byte(claims.CSRFToken)) != 1 {
				if audit != nil {
					audit.Log(AuditEvent{
						Event:    "csrf_mismatch",
						ClientIP: r.RemoteAddr,
						Method:   r.Method,
						Path:     r.URL.Path,
						Outcome:  "denied",
					})
				}
				http.Error(w, "Forbidden: invalid CSRF token", http.StatusForbidden)
				return
			}
		}

		next(w, r, claims)
	}
}

// WriteRateLimitExceeded writes a standard 429 response with Retry-After header.
func WriteRateLimitExceeded(w http.ResponseWriter, retryAfterSec int) {
	w.Header().Set("Retry-After", strconv.Itoa(retryAfterSec))
	http.Error(w, "Too many failed authentication attempts; try again later", http.StatusTooManyRequests)
}

func (sm *SessionManager) sign(payload string) string {
	mac := hmac.New(sha256.New, sm.secret)
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func randomHex(numBytes int) (string, error) {
	b := make([]byte, numBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
