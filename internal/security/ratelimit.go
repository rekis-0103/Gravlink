package security

import (
	"sync"
	"time"
)

// AuthRateLimiter tracks failed authentication attempts per IP within a sliding window.
type AuthRateLimiter struct {
	mu          sync.Mutex
	maxFailures int
	window      time.Duration
	failures    map[string][]time.Time
}

// NewAuthRateLimiter creates a limiter allowing maxFailures within window per IP.
func NewAuthRateLimiter(maxFailures int, window time.Duration) *AuthRateLimiter {
	if maxFailures <= 0 {
		maxFailures = 5
	}
	if window <= 0 {
		window = 15 * time.Minute
	}
	return &AuthRateLimiter{
		maxFailures: maxFailures,
		window:      window,
		failures:    make(map[string][]time.Time),
	}
}

// Allow checks if ip is currently allowed to attempt authentication.
// Returns (allowed, retryAfterSeconds).
func (rl *AuthRateLimiter) Allow(ip string) (bool, int) {
	now := time.Now()
	cutoff := now.Add(-rl.window)

	rl.mu.Lock()
	defer rl.mu.Unlock()

	recent := pruneOld(rl.failures[ip], cutoff)
	if len(recent) == 0 {
		delete(rl.failures, ip)
		return true, 0
	}
	rl.failures[ip] = recent

	if len(recent) >= rl.maxFailures {
		oldest := recent[0]
		retryAfter := int(oldest.Add(rl.window).Sub(now).Seconds())
		if retryAfter < 1 {
			retryAfter = 1
		}
		return false, retryAfter
	}
	return true, 0
}

// RecordFailure records a failed authentication attempt for ip.
func (rl *AuthRateLimiter) RecordFailure(ip string) {
	now := time.Now()
	cutoff := now.Add(-rl.window)

	rl.mu.Lock()
	defer rl.mu.Unlock()

	recent := pruneOld(rl.failures[ip], cutoff)
	recent = append(recent, now)
	rl.failures[ip] = recent
}

// Reset clears failure history for ip after a successful authentication.
func (rl *AuthRateLimiter) Reset(ip string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	delete(rl.failures, ip)
}

func pruneOld(ts []time.Time, cutoff time.Time) []time.Time {
	if len(ts) == 0 {
		return nil
	}
	idx := 0
	for idx < len(ts) && ts[idx].Before(cutoff) {
		idx++
	}
	if idx == len(ts) {
		return nil
	}
	return append([]time.Time(nil), ts[idx:]...)
}
