package internaltls_test

import (
	"crypto/tls"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/0xHoaxen/shogun/pkg/internaltls"
	"github.com/0xHoaxen/shogun/pkg/internaltls/internaltlstest"
)

func lookupOf(env map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) { v, ok := env[k]; return v, ok }
}

func TestLoadIsOffInLocalDevelopmentWhenNothingIsSet(t *testing.T) {
	got, err := internaltls.Load(lookupOf(map[string]string{"ENVIRONMENT": "local"}))

	if err != nil || got != nil {
		t.Fatalf("got %v, %v; want no TLS and no error", got, err)
	}
}

func TestLoadFailsClosedWhenProductionHasNoCertificates(t *testing.T) {
	for _, env := range []string{"production", "staging"} {
		t.Run(env, func(t *testing.T) {
			_, err := internaltls.Load(lookupOf(map[string]string{"ENVIRONMENT": env}))

			if err == nil || !strings.Contains(err.Error(), "INTERNAL_TLS_CA_FILE") {
				t.Fatalf("err = %v, want a refusal naming the missing setting", err)
			}
		})
	}
}

func TestLoadRefusesAHalfConfiguredSetup(t *testing.T) {
	dir := t.TempDir()
	env := internaltlstest.NewCA(t, "ca").Issue(t, dir, "svc", time.Now().Add(time.Hour))
	delete(env, "INTERNAL_TLS_KEY_FILE")
	env["ENVIRONMENT"] = "local"

	_, err := internaltls.Load(lookupOf(env))

	if err == nil || !strings.Contains(err.Error(), "INTERNAL_TLS_KEY_FILE") {
		t.Fatalf("err = %v, want the missing key named even in local development", err)
	}
}

func TestLoadRejectsFilesThatAreNotUsable(t *testing.T) {
	dir := t.TempDir()
	env := internaltlstest.NewCA(t, "ca").Issue(t, dir, "svc", time.Now().Add(time.Hour))
	env["ENVIRONMENT"] = "local"
	if err := os.WriteFile(env["INTERNAL_TLS_CA_FILE"], []byte("not a certificate"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := internaltls.Load(lookupOf(env)); err == nil {
		t.Fatal("Load accepted a CA file with no certificate in it")
	}
	env["INTERNAL_TLS_CA_FILE"] = filepath.Join(dir, "missing.pem")
	if _, err := internaltls.Load(lookupOf(env)); err == nil {
		t.Fatal("Load accepted a CA file that does not exist")
	}
}

func TestServerAndClientConfigsAreStrict(t *testing.T) {
	dir := t.TempDir()
	env := internaltlstest.NewCA(t, "ca").Issue(t, dir, "svc", time.Now().Add(time.Hour))
	env["ENVIRONMENT"] = "production"
	cfg, err := internaltls.Load(lookupOf(env))
	if err != nil || cfg == nil {
		t.Fatalf("Load: %v, %v", cfg, err)
	}

	server, client := cfg.Server(), cfg.Client()

	if server.MinVersion != tls.VersionTLS13 || client.MinVersion != tls.VersionTLS13 {
		t.Fatalf("min versions %x and %x, want TLS 1.3", server.MinVersion, client.MinVersion)
	}
	if server.ClientAuth != tls.RequireAndVerifyClientCert {
		t.Fatalf("server client auth = %v, want mutual TLS", server.ClientAuth)
	}
	if client.InsecureSkipVerify || server.InsecureSkipVerify { //nolint:gosec // asserting it is off
		t.Fatal("certificate verification is switched off")
	}
}

// handshake connects a client config to a server config over loopback TCP.
func handshake(t *testing.T, server, client *tls.Config, serverName string) (serverErr, clientErr error) {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = lis.Close() })
	done := make(chan error, 1)
	go func() {
		conn, err := lis.Accept()
		if err != nil {
			done <- err
			return
		}
		defer func() { _ = conn.Close() }()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		s := tls.Server(conn, server)
		if err := s.Handshake(); err != nil {
			done <- err
			return
		}
		// TLS 1.3 lets a client finish before the server has checked its certificate;
		// the server's verdict arrives with the first read.
		_, err = s.Read(make([]byte, 1))
		if err != nil && err.Error() == "EOF" {
			err = nil
		}
		done <- err
	}()
	raw, err := net.DialTimeout("tcp", lis.Addr().String(), 5*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = raw.Close() }()
	_ = raw.SetDeadline(time.Now().Add(5 * time.Second))
	cc := client.Clone()
	cc.ServerName = serverName
	c := tls.Client(raw, cc)
	if clientErr = c.Handshake(); clientErr == nil {
		_, _ = c.Write([]byte{1})
		// Read surfaces an alert the server sent after checking our certificate.
		if _, err := c.Read(make([]byte, 1)); err != nil && err.Error() != "EOF" {
			clientErr = err
		}
	}
	_ = raw.Close()
	return <-done, clientErr
}

func TestServicesOfOneCAHandshakeBothWays(t *testing.T) {
	ca := internaltlstest.NewCA(t, "ca")
	srv, cli := t.TempDir(), t.TempDir()
	srvEnv := ca.Issue(t, srv, "soroban", time.Now().Add(time.Hour))
	cliEnv := ca.Issue(t, cli, "torii", time.Now().Add(time.Hour))
	srvEnv["ENVIRONMENT"], cliEnv["ENVIRONMENT"] = "production", "production"
	server, err := internaltls.Load(lookupOf(srvEnv))
	if err != nil {
		t.Fatal(err)
	}
	client, err := internaltls.Load(lookupOf(cliEnv))
	if err != nil {
		t.Fatal(err)
	}

	serverErr, clientErr := handshake(t, server.Server(), client.Client(), "soroban")

	if serverErr != nil || clientErr != nil {
		t.Fatalf("server %v, client %v; want a mutual handshake", serverErr, clientErr)
	}
}

func TestAClientFromAnotherCAIsRefused(t *testing.T) {
	ours, theirs := internaltlstest.NewCA(t, "ours"), internaltlstest.NewCA(t, "theirs")
	srvEnv := ours.Issue(t, t.TempDir(), "soroban", time.Now().Add(time.Hour))
	strangerEnv := theirs.Issue(t, t.TempDir(), "torii", time.Now().Add(time.Hour))
	srvEnv["ENVIRONMENT"], strangerEnv["ENVIRONMENT"] = "production", "production"
	server, _ := internaltls.Load(lookupOf(srvEnv))
	// The stranger trusts our CA to verify the server but presents a certificate we did not issue.
	strangerEnv["INTERNAL_TLS_CA_FILE"] = srvEnv["INTERNAL_TLS_CA_FILE"]
	stranger, err := internaltls.Load(lookupOf(strangerEnv))
	if err != nil {
		t.Fatal(err)
	}

	serverErr, _ := handshake(t, server.Server(), stranger.Client(), "soroban")

	if serverErr == nil {
		t.Fatal("the server accepted a client certificate from another CA")
	}
}

func TestAServerFromAnotherCAIsRefused(t *testing.T) {
	ours, theirs := internaltlstest.NewCA(t, "ours"), internaltlstest.NewCA(t, "theirs")
	cliEnv := ours.Issue(t, t.TempDir(), "torii", time.Now().Add(time.Hour))
	impostorEnv := theirs.Issue(t, t.TempDir(), "soroban", time.Now().Add(time.Hour))
	cliEnv["ENVIRONMENT"], impostorEnv["ENVIRONMENT"] = "production", "production"
	client, _ := internaltls.Load(lookupOf(cliEnv))
	// The impostor trusts our CA, so only the client can tell it apart.
	impostorEnv["INTERNAL_TLS_CA_FILE"] = cliEnv["INTERNAL_TLS_CA_FILE"]
	impostor, err := internaltls.Load(lookupOf(impostorEnv))
	if err != nil {
		t.Fatal(err)
	}

	_, clientErr := handshake(t, impostor.Server(), client.Client(), "soroban")

	if clientErr == nil {
		t.Fatal("the client trusted a server certificate from another CA")
	}
}

func TestAServerCertificateForAnotherNameIsRefused(t *testing.T) {
	ca := internaltlstest.NewCA(t, "ca")
	srvEnv := ca.Issue(t, t.TempDir(), "fude", time.Now().Add(time.Hour))
	cliEnv := ca.Issue(t, t.TempDir(), "torii", time.Now().Add(time.Hour))
	srvEnv["ENVIRONMENT"], cliEnv["ENVIRONMENT"] = "production", "production"
	server, _ := internaltls.Load(lookupOf(srvEnv))
	client, _ := internaltls.Load(lookupOf(cliEnv))

	_, clientErr := handshake(t, server.Server(), client.Client(), "soroban")

	if clientErr == nil {
		t.Fatal("the client accepted fude's certificate when dialling soroban")
	}
}

func TestACertificateRenewedOnDiskIsPickedUpWithoutARestart(t *testing.T) {
	ca := internaltlstest.NewCA(t, "ca")
	srvDir := t.TempDir()
	srvEnv := ca.Issue(t, srvDir, "soroban", time.Now().Add(time.Hour))
	cliEnv := ca.Issue(t, t.TempDir(), "torii", time.Now().Add(time.Hour))
	srvEnv["ENVIRONMENT"], cliEnv["ENVIRONMENT"] = "production", "production"
	server, _ := internaltls.Load(lookupOf(srvEnv))
	client, _ := internaltls.Load(lookupOf(cliEnv))
	serial := func() string {
		lis, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = lis.Close() }()
		go func() {
			conn, err := lis.Accept()
			if err != nil {
				return
			}
			defer func() { _ = conn.Close() }()
			s := tls.Server(conn, server.Server())
			_ = s.Handshake()
			_, _ = s.Read(make([]byte, 1))
		}()
		raw, err := net.Dial("tcp", lis.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = raw.Close() }()
		_ = raw.SetDeadline(time.Now().Add(5 * time.Second))
		cc := client.Client().Clone()
		cc.ServerName = "soroban"
		c := tls.Client(raw, cc)
		if err := c.Handshake(); err != nil {
			t.Fatalf("handshake: %v", err)
		}
		return c.ConnectionState().PeerCertificates[0].SerialNumber.String()
	}
	before := serial()

	ca.Issue(t, srvDir, "soroban", time.Now().Add(2*time.Hour)) // renews the files in place
	after := serial()

	if before == after {
		t.Fatal("the server kept presenting the old certificate after it was renewed on disk")
	}
}
