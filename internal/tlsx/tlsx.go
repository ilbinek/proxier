// Package tlsx builds the TLS configuration for the web UI.
package tlsx

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"log/slog"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/crypto/acme/autocert"

	"proxier/internal/config"
)

// Config returns nil for TLS mode "off".
func Config(cfg *config.Config, log *slog.Logger, extraIPs []string) (*tls.Config, error) {
	switch cfg.TLSMode {
	case config.TLSOff:
		return nil, nil
	case config.TLSSelfSigned:
		cert, err := selfSigned(cfg.DataDir, extraIPs, log)
		if err != nil {
			return nil, err
		}
		return &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{cert}}, nil
	case config.TLSACME:
		m := &autocert.Manager{
			Prompt:     autocert.AcceptTOS,
			HostPolicy: autocert.HostWhitelist(cfg.ACMEDomain),
			Cache:      autocert.DirCache(filepath.Join(cfg.DataDir, "acme")),
			Email:      cfg.ACMEEmail,
		}
		// TLSConfig enables the tls-alpn-01 challenge, so no port 80 is needed,
		// but the UI must be reachable on port 443.
		tc := m.TLSConfig()
		tc.MinVersion = tls.VersionTLS12
		log.Info("ACME enabled", "domain", cfg.ACMEDomain, "note", "UI must be reachable on :443 for tls-alpn-01")
		return tc, nil
	case config.TLSFiles:
		r := &reloader{certFile: cfg.TLSCert, keyFile: cfg.TLSKey}
		if _, err := r.get(nil); err != nil {
			return nil, err
		}
		return &tls.Config{MinVersion: tls.VersionTLS12, GetCertificate: r.get}, nil
	}
	return nil, fmt.Errorf("unknown TLS mode %q", cfg.TLSMode)
}

// reloader re-reads certificate files when they change (e.g. certbot renewals).
type reloader struct {
	certFile, keyFile string
	mu                sync.Mutex
	cert              *tls.Certificate
	modTime           time.Time
	checked           time.Time
}

func (r *reloader) get(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cert != nil && time.Since(r.checked) < time.Minute {
		return r.cert, nil
	}
	r.checked = time.Now()
	st, err := os.Stat(r.certFile)
	if err != nil {
		if r.cert != nil {
			return r.cert, nil
		}
		return nil, err
	}
	if r.cert != nil && st.ModTime().Equal(r.modTime) {
		return r.cert, nil
	}
	c, err := tls.LoadX509KeyPair(r.certFile, r.keyFile)
	if err != nil {
		if r.cert != nil {
			return r.cert, nil
		}
		return nil, err
	}
	r.cert, r.modTime = &c, st.ModTime()
	return r.cert, nil
}

func selfSigned(dataDir string, ips []string, log *slog.Logger) (tls.Certificate, error) {
	certPath := filepath.Join(dataDir, "selfsigned.crt")
	keyPath := filepath.Join(dataDir, "selfsigned.key")
	if c, err := tls.LoadX509KeyPair(certPath, keyPath); err == nil {
		if leaf, err := x509.ParseCertificate(c.Certificate[0]); err == nil && time.Until(leaf.NotAfter) > 30*24*time.Hour {
			return c, nil
		}
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 126))
	host, _ := os.Hostname()
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "proxier", Organization: []string{"proxier self-signed"}},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(5, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
	}
	if host != "" {
		tmpl.DNSNames = append(tmpl.DNSNames, host)
	}
	for _, s := range ips {
		if ip := net.ParseIP(s); ip != nil {
			tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}
	kb, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return tls.Certificate{}, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kb})
	if err := os.WriteFile(certPath, certPEM, 0o644); err != nil {
		return tls.Certificate{}, err
	}
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		return tls.Certificate{}, err
	}
	log.Info("generated self-signed certificate", "path", certPath)
	return tls.X509KeyPair(certPEM, keyPEM)
}
