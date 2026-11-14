package security

import (
	"crypto/x509"
	"encoding/pem"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHashAndVerifyPassword(t *testing.T) {
	hash, err := HashPassword("gravlink-secret-pass")
	if err != nil {
		t.Fatalf("HashPassword failed: %v", err)
	}
	ok, err := VerifyPassword("gravlink-secret-pass", hash)
	if err != nil || !ok {
		t.Fatalf("VerifyPassword expected true, got ok=%v err=%v", ok, err)
	}
	ok, err = VerifyPassword("wrong-password", hash)
	if err != nil || ok {
		t.Fatalf("VerifyPassword expected false for wrong password, got ok=%v err=%v", ok, err)
	}
}

func TestSessionAndPairingToken(t *testing.T) {
	sm, err := NewSessionManager("00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff", time.Hour, true)
	if err != nil {
		t.Fatalf("NewSessionManager failed: %v", err)
	}

	rec := httptest.NewRecorder()
	claims, err := sm.IssueSession(rec, "192.168.1.50")
	if err != nil {
		t.Fatalf("IssueSession failed: %v", err)
	}
	res := rec.Result()
	cookies := res.Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || !cookies[0].Secure {
		t.Fatalf("expected 1 HttpOnly+Secure cookie, got %+v", cookies)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/conversations", nil)
	req.AddCookie(cookies[0])
	verified, ok := sm.VerifyRequest(req)
	if !ok || verified.SessionID != claims.SessionID {
		t.Fatalf("expected valid session, got ok=%v", ok)
	}

	// Tampered cookie must fail
	badCookie := *cookies[0]
	badCookie.Value = badCookie.Value + "x"
	badReq := httptest.NewRequest(http.MethodGet, "/api/conversations", nil)
	badReq.AddCookie(&badCookie)
	if _, ok := sm.VerifyRequest(badReq); ok {
		t.Fatal("tampered cookie should be rejected")
	}

	// Pairing token is single-use
	tok, err := sm.CreatePairingToken(time.Minute)
	if err != nil {
		t.Fatalf("CreatePairingToken failed: %v", err)
	}
	if !sm.ConsumePairingToken(tok) {
		t.Fatal("expected first pairing token consumption to succeed")
	}
	if sm.ConsumePairingToken(tok) {
		t.Fatal("expected second pairing token consumption to fail")
	}
}

func TestIPFilterAndSameOrigin(t *testing.T) {
	_, subnet, _ := net.ParseCIDR("192.168.1.0/24")
	subnets := []*net.IPNet{subnet}

	if !IsAllowedLANIP(net.ParseIP("127.0.0.1"), subnets) {
		t.Fatal("loopback should be allowed")
	}
	if !IsAllowedLANIP(net.ParseIP("192.168.1.42"), subnets) {
		t.Fatal("same LAN subnet IP should be allowed")
	}
	if IsAllowedLANIP(net.ParseIP("10.0.0.5"), subnets) {
		t.Fatal("different subnet should be blocked when strict subnets are configured")
	}
	if IsAllowedLANIP(net.ParseIP("8.8.8.8"), nil) {
		t.Fatal("public IP must always be blocked")
	}

	handler := IPFilterMiddleware(subnets, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "8.8.8.8:54321"
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for public IP, got %d", rec.Code)
	}

	reqOrigin := httptest.NewRequest(http.MethodPost, "/api/test", nil)
	reqOrigin.Host = "192.168.1.10:8443"
	reqOrigin.Header.Set("Origin", "https://evil.example")
	if ValidateSameOrigin(reqOrigin) {
		t.Fatal("cross-origin request must be rejected")
	}
}

func TestRateLimiter(t *testing.T) {
	rl := NewAuthRateLimiter(3, time.Minute)
	ip := "192.168.1.25"
	for i := 0; i < 3; i++ {
		if ok, _ := rl.Allow(ip); !ok {
			t.Fatalf("attempt %d should be allowed", i)
		}
		rl.RecordFailure(ip)
	}
	if ok, retry := rl.Allow(ip); ok || retry < 1 {
		t.Fatalf("expected rate limit block after 3 failures, got ok=%v retry=%d", ok, retry)
	}
	rl.Reset(ip)
	if ok, _ := rl.Allow(ip); !ok {
		t.Fatal("expected reset to allow attempts again")
	}
}

func TestEnsureLocalTLS(t *testing.T) {
	dir := t.TempDir()
	lanIP := net.ParseIP("192.168.1.99")
	cert, caPEM, err := EnsureLocalTLS(dir, []net.IP{lanIP})
	if err != nil {
		t.Fatalf("EnsureLocalTLS failed: %v", err)
	}
	if len(cert.Certificate) == 0 || len(caPEM) == 0 {
		t.Fatal("expected generated leaf cert and CA PEM")
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatalf("ParseCertificate failed: %v", err)
	}
	found := false
	for _, ip := range leaf.IPAddresses {
		if ip.Equal(lanIP) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected LAN IP %v in cert SANs, got %v", lanIP, leaf.IPAddresses)
	}
	if block, _ := pem.Decode(caPEM); block == nil {
		t.Fatal("invalid CA PEM")
	}
}

func TestInterfaceScoringPrefersWiFiOverVirtualBox(t *testing.T) {
	wifiIP := net.ParseIP("192.168.1.11").To4()
	vboxIP := net.ParseIP("192.168.56.1").To4()

	wifiScore := scoreInterface("Wi-Fi", wifiIP, wifiIP)
	vboxScore := scoreInterface("Ethernet 2", vboxIP, wifiIP)
	if wifiScore <= vboxScore {
		t.Fatalf("expected Wi-Fi score (%d) > VirtualBox score (%d)", wifiScore, vboxScore)
	}
}

