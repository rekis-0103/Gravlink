package security

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"slices"
	"time"
)

// EnsureLocalTLS creates or loads a local Root CA and a LAN-scoped TLS server certificate.
func EnsureLocalTLS(dataDir string, lanIPs []net.IP) (tls.Certificate, []byte, error) {
	certsDir := filepath.Join(dataDir, "certs")
	if err := os.MkdirAll(certsDir, 0o700); err != nil {
		return tls.Certificate{}, nil, err
	}

	caCertPath := filepath.Join(certsDir, "ca.crt")
	caKeyPath := filepath.Join(certsDir, "ca.key")
	caCert, caKey, caPEM, err := loadOrCreateCA(caCertPath, caKeyPath)
	if err != nil {
		return tls.Certificate{}, nil, err
	}

	srvCertPath := filepath.Join(certsDir, "server.crt")
	srvKeyPath := filepath.Join(certsDir, "server.key")

	desiredIPs := collectCertIPs(lanIPs)
	if certNeedsRefresh(srvCertPath, desiredIPs) {
		if err := createLeafCert(srvCertPath, srvKeyPath, caCert, caKey, desiredIPs); err != nil {
			return tls.Certificate{}, nil, err
		}
	}

	tlsCert, err := tls.LoadX509KeyPair(srvCertPath, srvKeyPath)
	if err != nil {
		return tls.Certificate{}, nil, err
	}
	return tlsCert, caPEM, nil
}

func loadOrCreateCA(certPath, keyPath string) (*x509.Certificate, *ecdsa.PrivateKey, []byte, error) {
	certPEM, certErr := os.ReadFile(certPath)
	keyPEM, keyErr := os.ReadFile(keyPath)
	if certErr == nil && keyErr == nil {
		block, _ := pem.Decode(certPEM)
		keyBlock, _ := pem.Decode(keyPEM)
		if block != nil && keyBlock != nil {
			cert, err1 := x509.ParseCertificate(block.Bytes)
			key, err2 := x509.ParseECPrivateKey(keyBlock.Bytes)
			if err1 == nil && err2 == nil && time.Now().Before(cert.NotAfter.Add(-30*24*time.Hour)) {
				return cert, key, certPEM, nil
			}
		}
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, nil, err
	}
	serial, err := randSerial()
	if err != nil {
		return nil, nil, nil, err
	}
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   "Gravlink Local Root CA",
			Organization: []string{"Gravlink Local LAN"},
		},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(5 * 365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            0,
		MaxPathLenZero:        true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, nil, nil, err
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, nil, nil, err
	}

	certOut := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, nil, nil, err
	}
	keyOut := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	if err := os.WriteFile(certPath, certOut, 0o644); err != nil {
		return nil, nil, nil, err
	}
	if err := os.WriteFile(keyPath, keyOut, 0o600); err != nil {
		return nil, nil, nil, err
	}
	return cert, key, certOut, nil
}

func collectCertIPs(lanIPs []net.IP) []net.IP {
	ips := []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")}
	for _, ip := range lanIPs {
		norm := normalizeIP(ip)
		if norm == nil {
			continue
		}
		if !slices.ContainsFunc(ips, func(existing net.IP) bool { return existing.Equal(norm) }) {
			ips = append(ips, norm)
		}
	}
	return ips
}

func certNeedsRefresh(certPath string, desiredIPs []net.IP) bool {
	raw, err := os.ReadFile(certPath)
	if err != nil {
		return true
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		return true
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return true
	}
	if time.Now().After(cert.NotAfter.Add(-7 * 24 * time.Hour)) {
		return true
	}
	for _, want := range desiredIPs {
		if !slices.ContainsFunc(cert.IPAddresses, func(have net.IP) bool { return have.Equal(want) }) {
			return true
		}
	}
	return false
}

func createLeafCert(certPath, keyPath string, caCert *x509.Certificate, caKey *ecdsa.PrivateKey, ips []net.IP) error {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	serial, err := randSerial()
	if err != nil {
		return err
	}
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   "gravlink.local",
			Organization: []string{"Gravlink Local LAN"},
		},
		NotBefore:   now.Add(-time.Hour),
		NotAfter:    now.Add(90 * 24 * time.Hour),
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:    []string{"localhost", "gravlink.local"},
		IPAddresses: ips,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, caCert, &key.PublicKey, caKey)
	if err != nil {
		return err
	}
	certOut := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return err
	}
	keyOut := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	if err := os.WriteFile(certPath, certOut, 0o644); err != nil {
		return err
	}
	return os.WriteFile(keyPath, keyOut, 0o600)
}

func randSerial() (*big.Int, error) {
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	return rand.Int(rand.Reader, limit)
}
