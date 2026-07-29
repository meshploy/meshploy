package k8s

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// newCACertPEM generates a throwaway self-signed certificate. client-go parses
// the CA when building a client, so a placeholder string will not do.
func newCACertPEM(t *testing.T) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

// These tests pin how TLS is configured when K3S_SERVER_URL rewrites the API
// server address.
//
// Regression guard: the rewrite used to set Insecure and discard the CA, which
// silently disabled authentication of a connection carrying cluster-admin
// credentials — on the standard Docker deployment, which sets K3S_SERVER_URL by
// default. No cluster is needed here: everything asserted is config assembly,
// which is exactly where the bug lived.

// A syntactically valid kubeconfig carrying a CA, so the loader produces the
// same shape it would in production. The CA is never validated here.
func writeKubeconfig(t *testing.T) string {
	t.Helper()
	ca := base64.StdEncoding.EncodeToString(newCACertPEM(t))
	body := `apiVersion: v1
kind: Config
clusters:
- name: default
  cluster:
    server: https://127.0.0.1:6443
    certificate-authority-data: ` + ca + `
contexts:
- name: default
  context:
    cluster: default
    user: default
current-context: default
users:
- name: default
  user:
    token: fake-token
`
	path := filepath.Join(t.TempDir(), "kubeconfig")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}
	return path
}

// The core regression: rewriting the address must not stop the certificate
// being checked.
func TestServerURLOverrideKeepsVerificationOn(t *testing.T) {
	_, cfg, err := NewClientWithOptions(Options{
		KubeconfigPath: writeKubeconfig(t),
		ServerURL:      "https://host.meshploy.internal:6443",
	})
	if err != nil {
		t.Fatalf("build client: %v", err)
	}

	if cfg.Host != "https://host.meshploy.internal:6443" {
		t.Fatalf("the server address should be overridden, got %q", cfg.Host)
	}
	if cfg.TLSClientConfig.Insecure {
		t.Fatal("overriding the server address must not disable certificate verification")
	}
	if len(cfg.TLSClientConfig.CAData) == 0 && cfg.TLSClientConfig.CAFile == "" {
		t.Fatal("the cluster CA must be retained, or nothing authenticates the server")
	}
	// Without this the handshake fails: the cluster certificate is not issued
	// for the rewritten address.
	if cfg.TLSClientConfig.ServerName != DefaultTLSServerName {
		t.Fatalf("ServerName should be pinned to %q, got %q", DefaultTLSServerName, cfg.TLSClientConfig.ServerName)
	}
}

func TestTLSServerNameIsOverridable(t *testing.T) {
	_, cfg, err := NewClientWithOptions(Options{
		KubeconfigPath: writeKubeconfig(t),
		ServerURL:      "https://host.meshploy.internal:6443",
		TLSServerName:  "k3s.example.internal",
	})
	if err != nil {
		t.Fatalf("build client: %v", err)
	}
	if cfg.TLSClientConfig.ServerName != "k3s.example.internal" {
		t.Fatalf("an explicit server name must win, got %q", cfg.TLSClientConfig.ServerName)
	}
	if cfg.TLSClientConfig.Insecure {
		t.Fatal("a custom server name must not imply skipping verification")
	}
}

// The escape hatch still works, and still drops the CA — client-go rejects a
// config that sets both.
func TestSkipTLSVerifyIsExplicitOnly(t *testing.T) {
	_, cfg, err := NewClientWithOptions(Options{
		KubeconfigPath: writeKubeconfig(t),
		ServerURL:      "https://host.meshploy.internal:6443",
		SkipTLSVerify:  true,
	})
	if err != nil {
		t.Fatalf("build client: %v", err)
	}
	if !cfg.TLSClientConfig.Insecure {
		t.Fatal("SkipTLSVerify must disable verification when explicitly set")
	}
	if len(cfg.TLSClientConfig.CAData) != 0 || cfg.TLSClientConfig.CAFile != "" {
		t.Fatal("client-go rejects Insecure together with a CA, so the CA must be cleared")
	}
}

// Without an override there is nothing to reconcile: the kubeconfig is used as
// written, including its own CA and address.
func TestNoOverrideLeavesTheKubeconfigAlone(t *testing.T) {
	_, cfg, err := NewClientWithOptions(Options{KubeconfigPath: writeKubeconfig(t)})
	if err != nil {
		t.Fatalf("build client: %v", err)
	}
	if cfg.Host != "https://127.0.0.1:6443" {
		t.Fatalf("host should come from the kubeconfig, got %q", cfg.Host)
	}
	if cfg.TLSClientConfig.Insecure {
		t.Fatal("verification must be on when no override is in play")
	}
	if cfg.TLSClientConfig.ServerName != "" {
		t.Fatalf("no name should be pinned without an override, got %q", cfg.TLSClientConfig.ServerName)
	}
}

// The old two-argument entry point must carry the same guarantee — it is what
// any out-of-tree caller still uses.
func TestLegacyConstructorAlsoVerifies(t *testing.T) {
	_, cfg, err := NewClientWithConfig(writeKubeconfig(t), "https://host.meshploy.internal:6443")
	if err != nil {
		t.Fatalf("build client: %v", err)
	}
	if cfg.TLSClientConfig.Insecure {
		t.Fatal("NewClientWithConfig must not disable verification either")
	}
	if cfg.TLSClientConfig.ServerName != DefaultTLSServerName {
		t.Fatalf("ServerName should be pinned, got %q", cfg.TLSClientConfig.ServerName)
	}
}
