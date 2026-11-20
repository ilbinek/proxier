// Package config holds the settings for proxier's own HTTP server. These come
// from flags and environment variables (PROXIER_*), never from the database,
// so a mistake in the web UI can never lock the operator out.
package config

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

type TLSMode string

const (
	TLSOff        TLSMode = "off"
	TLSSelfSigned TLSMode = "self-signed"
	TLSACME       TLSMode = "acme"
	TLSFiles      TLSMode = "files"
)

type Config struct {
	DataDir    string
	Listen     string
	TLSMode    TLSMode
	ACMEDomain string
	ACMEEmail  string
	TLSCert    string
	TLSKey     string
	LogLevel   string
	// TrustProxy makes the server honour X-Forwarded-For / X-Forwarded-Proto
	// (enable only when running behind your own reverse proxy).
	TrustProxy bool
}

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

// Parse parses flags from args using env vars as defaults.
func Parse(args []string) (*Config, error) {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	c := &Config{}
	var tlsMode string
	fs.StringVar(&c.DataDir, "data-dir", env("PROXIER_DATA_DIR", "./data"), "directory for the database and generated certificates")
	fs.StringVar(&c.Listen, "listen", env("PROXIER_LISTEN", ":8080"), "address for the web UI (e.g. 127.0.0.1:8080 or :8443)")
	fs.StringVar(&tlsMode, "tls", env("PROXIER_TLS", "off"), "TLS mode: off | self-signed | acme | files")
	fs.StringVar(&c.ACMEDomain, "acme-domain", env("PROXIER_ACME_DOMAIN", ""), "domain for Let's Encrypt (tls=acme)")
	fs.StringVar(&c.ACMEEmail, "acme-email", env("PROXIER_ACME_EMAIL", ""), "contact email for Let's Encrypt (tls=acme)")
	fs.StringVar(&c.TLSCert, "tls-cert", env("PROXIER_TLS_CERT", ""), "certificate file (tls=files)")
	fs.StringVar(&c.TLSKey, "tls-key", env("PROXIER_TLS_KEY", ""), "private key file (tls=files)")
	fs.StringVar(&c.LogLevel, "log-level", env("PROXIER_LOG_LEVEL", "info"), "debug | info | warn | error")
	fs.BoolVar(&c.TrustProxy, "trust-proxy", env("PROXIER_TRUST_PROXY", "false") == "true", "trust X-Forwarded-For/Proto headers (behind a reverse proxy)")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	c.TLSMode = TLSMode(strings.ToLower(tlsMode))
	return c, c.Validate()
}

func (c *Config) Validate() error {
	switch c.TLSMode {
	case TLSOff, TLSSelfSigned:
	case TLSACME:
		if c.ACMEDomain == "" {
			return fmt.Errorf("--acme-domain is required with --tls=acme")
		}
	case TLSFiles:
		if c.TLSCert == "" || c.TLSKey == "" {
			return fmt.Errorf("--tls-cert and --tls-key are required with --tls=files")
		}
	default:
		return fmt.Errorf("unknown --tls mode %q", c.TLSMode)
	}
	return nil
}
