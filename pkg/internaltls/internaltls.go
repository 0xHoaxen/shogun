// Package internaltls secures the gRPC hops between services with mutual TLS.
//
// Every service holds a certificate issued by one internal CA. A server accepts
// only clients whose certificate that CA issued, and a client accepts only
// servers whose certificate that CA issued for the name it dialled, so a pod
// that is not one of ours can neither call a service nor pose as one. User API
// keys cross this network, so outside local development a service refuses to
// start or to dial without it.
//
// The certificate and key are read from their files on every handshake, so a
// renewed certificate (for example one written by cert-manager) is used without
// a restart. The CA bundle is read once at startup.
package internaltls

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/0xHoaxen/shogun/pkg/config"
)

// Environment variables that name the files.
const (
	CAFileEnv   = "INTERNAL_TLS_CA_FILE"
	CertFileEnv = "INTERNAL_TLS_CERT_FILE"
	KeyFileEnv  = "INTERNAL_TLS_KEY_FILE"
)

// Config is a service's TLS identity and the CA it trusts.
type Config struct {
	certFile, keyFile string
	pool              *x509.CertPool
}

// Load reads the TLS settings from the environment. It returns nil and no error
// only in local development or tests with nothing set, which means plaintext.
// Any other environment must be fully configured, and a partly configured setup
// is always an error, so a typo cannot silently switch security off.
func Load(lookup config.LookupFunc) (*Config, error) {
	caFile, certFile, keyFile := value(lookup, CAFileEnv), value(lookup, CertFileEnv), value(lookup, KeyFileEnv)
	if caFile == "" && certFile == "" && keyFile == "" {
		if plaintextAllowed(config.String(lookup, "ENVIRONMENT", config.EnvLocal)) {
			return nil, nil
		}
		return nil, fmt.Errorf("internaltls: %s, %s and %s are required outside local development", CAFileEnv, CertFileEnv, KeyFileEnv)
	}
	var missing []string
	for name, v := range map[string]string{CAFileEnv: caFile, CertFileEnv: certFile, KeyFileEnv: keyFile} {
		if v == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("internaltls: %s must be set together with the other TLS files", strings.Join(sortedStrings(missing), ", "))
	}
	pool, err := readPool(caFile)
	if err != nil {
		return nil, err
	}
	c := &Config{certFile: certFile, keyFile: keyFile, pool: pool}
	// Fail at startup, not at the first call, if the identity cannot be read.
	if _, err := c.keyPair(); err != nil {
		return nil, err
	}
	return c, nil
}

func plaintextAllowed(env string) bool { return env == config.EnvLocal || env == config.EnvTest }

func value(lookup config.LookupFunc, key string) string {
	v, _ := lookup(key)
	return strings.TrimSpace(v)
}

func sortedStrings(in []string) []string {
	out := append([]string(nil), in...)
	for i := range out {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

func readPool(path string) (*x509.CertPool, error) {
	pemBytes, err := os.ReadFile(path) //nolint:gosec // the path is operator configuration
	if err != nil {
		return nil, fmt.Errorf("internaltls: read %s: %w", CAFileEnv, err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pemBytes) {
		return nil, errors.New("internaltls: " + CAFileEnv + " holds no certificate")
	}
	return pool, nil
}

func (c *Config) keyPair() (*tls.Certificate, error) {
	pair, err := tls.LoadX509KeyPair(c.certFile, c.keyFile)
	if err != nil {
		return nil, fmt.Errorf("internaltls: load certificate: %w", err)
	}
	return &pair, nil
}

// Server is the config for a gRPC server: TLS 1.3, and a client certificate
// issued by the internal CA is required.
func (c *Config) Server() *tls.Config {
	return &tls.Config{
		MinVersion:     tls.VersionTLS13,
		ClientAuth:     tls.RequireAndVerifyClientCert,
		ClientCAs:      c.pool,
		GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) { return c.keyPair() },
	}
}

// Client is the config for a gRPC client: TLS 1.3, the server's certificate must
// be issued by the internal CA for the name dialled, and this service presents
// its own certificate.
func (c *Config) Client() *tls.Config {
	return &tls.Config{
		MinVersion: tls.VersionTLS13,
		RootCAs:    c.pool,
		GetClientCertificate: func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
			return c.keyPair()
		},
	}
}
