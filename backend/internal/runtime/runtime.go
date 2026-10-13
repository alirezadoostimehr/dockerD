package runtime

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"dockerd.local/backend/internal/config"
	"dockerd.local/backend/internal/gateway"
	"dockerd.local/backend/internal/idle"
)

const Instance = "dockerd"

type Paths struct{ Home, Bundle, Resources string }

func (p Paths) LimaHome() string { return filepath.Join(p.Home, "lima") }
func (p Paths) DockerSocket() string {
	return filepath.Join(p.LimaHome(), Instance, "sock", "docker.sock")
}
func (p Paths) Socket() string        { return filepath.Join(p.Home, "control.sock") }
func (p Paths) GatewaySocket() string { return filepath.Join(p.Home, "docker.sock") }
func (p Paths) Kubeconfig() string    { return filepath.Join(p.Home, "kubeconfig") }
func (p Paths) RawKubeconfig() string { return filepath.Join(p.Home, "kubeconfig-internal") }
func (p Paths) Binary(name string) string {
	switch name {
	case "limactl":
		return filepath.Join(p.Bundle, "lima", "bin", "limactl")
	case "docker":
		return filepath.Join(p.Bundle, "docker", "docker")
	default:
		return filepath.Join(p.Bundle, "bin", name)
	}
}

type Manager struct {
	Paths          Paths
	mu             sync.Mutex
	progress       string
	auditOffset    int64
	auditPartial   string
	sessions       map[string]idle.Blocker
	infrastructure map[string]bool
	docker         *http.Client
}

func New(p Paths) *Manager {
	return &Manager{Paths: p, sessions: map[string]idle.Blocker{}, infrastructure: map[string]bool{}, docker: &http.Client{Transport: gateway.UnixTransport(p.DockerSocket()), Timeout: 15 * time.Second}}
}
func (m *Manager) Progress() string     { m.mu.Lock(); defer m.mu.Unlock(); return m.progress }
func (m *Manager) SetProgress(s string) { m.mu.Lock(); m.progress = s; m.mu.Unlock() }
func (m *Manager) Env() []string {
	return append(os.Environ(), "LIMA_HOME="+m.Paths.LimaHome(), "DOCKER_CLI_PLUGIN_EXTRA_DIRS="+filepath.Join(m.Paths.Bundle, "cli-plugins"))
}
func (m *Manager) Command(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, m.Paths.Binary(name), args...)
	cmd.Env = m.Env()
	var out, errout bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errout
	e := cmd.Run()
	if e != nil {
		return nil, fmt.Errorf("%s: %w: %s", name, e, errout.String())
	}
	return out.Bytes(), nil
}
func (m *Manager) Lima(ctx context.Context, args ...string) ([]byte, error) {
	return m.Command(ctx, "limactl", args...)
}
func (m *Manager) Shell(ctx context.Context, args ...string) ([]byte, error) {
	return m.Lima(ctx, append([]string{"shell", Instance, "--"}, args...)...)
}
func (m *Manager) Installed() bool { _, e := os.Stat(m.Paths.Binary("limactl")); return e == nil }
func (m *Manager) Exists() bool {
	_, e := os.Stat(filepath.Join(m.Paths.LimaHome(), Instance, "lima.yaml"))
	return e == nil
}
func (m *Manager) Running(ctx context.Context) bool {
	b, e := m.Lima(ctx, "list", "--json", Instance)
	if e != nil {
		return false
	}
	var v struct{ Status string }
	return json.Unmarshal(b, &v) == nil && v.Status == "Running"
}

func (m *Manager) Start(ctx context.Context, s config.Settings) error {
	if !m.Installed() {
		return errors.New("bundled runtime is missing; run make app to download and package it")
	}
	if e := os.MkdirAll(m.Paths.LimaHome(), 0700); e != nil {
		return e
	}
	if !m.Exists() {
		m.SetProgress("Preparing the Linux virtual machine…")
		b, e := m.Template(s)
		if e != nil {
			return e
		}
		path := filepath.Join(m.Paths.Home, "instance.yaml")
		if e = os.WriteFile(path, b, 0600); e != nil {
			return e
		}
		if _, e = m.Lima(ctx, "create", "--name="+Instance, "--tty=false", path); e != nil {
			return e
		}
	} else if !m.Running(ctx) {
		// Lima applies stopped-instance resource and mount edits at the next start.
		mounts := []map[string]any{}
		for _, p := range s.SharedFolders {
			mounts = append(mounts, map[string]any{"location": p, "writable": true})
		}
		b, _ := json.Marshal(mounts)
		expr := fmt.Sprintf(".cpus = %d | .memory = \"%dGiB\" | .mounts = %s", s.CPUs, s.MemoryGiB, b)
		if _, e := m.Lima(ctx, "edit", "--tty=false", "--set", expr, Instance); e != nil {
			return e
		}
	}
	m.SetProgress("Starting VM · first launch downloads and verifies Linux and engine components…")
	cmd := exec.CommandContext(ctx, m.Paths.Binary("limactl"), "start", "--tty=false", Instance)
	cmd.Env = m.Env()
	log, e := os.OpenFile(filepath.Join(m.Paths.Home, "runtime.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if e != nil {
		return e
	}
	defer log.Close()
	cmd.Stdout = log
	cmd.Stderr = log
	if e = cmd.Run(); e != nil {
		return fmt.Errorf("VM startup failed: %w. See runtime.log in diagnostics", e)
	}
	m.SetProgress("Waiting for Docker Engine…")
	if _, e = m.Shell(ctx, "sudo", "systemctl", "start", "docker"); e != nil {
		return e
	}
	if e = retry(ctx, 90, func() error { _, e := m.Docker(ctx, "GET", "/_ping", nil); return e }); e != nil {
		return e
	}
	if s.Kubernetes {
		m.SetProgress("Starting Kubernetes and establishing activity monitoring…")
		if _, e = m.Shell(ctx, "sudo", "systemctl", "stop", "k3s"); e != nil {
			return e
		}
		if _, e = m.Shell(ctx, "sudo", "sh", "-c", "mkdir -p /var/log/dockerd; : > /var/log/dockerd/audit.log"); e != nil {
			return e
		}
		if _, e = m.Shell(ctx, "sudo", "systemctl", "start", "k3s"); e != nil {
			return e
		}
		if e = retry(ctx, 120, func() error {
			_, e := m.Shell(ctx, "sudo", "/usr/local/bin/k3s", "kubectl", "get", "--raw=/readyz")
			return e
		}); e != nil {
			return e
		}
		b, e := m.Shell(ctx, "sudo", "/usr/local/bin/k3s", "kubectl", "config", "view", "--raw", "-o", "json")
		if e != nil {
			return e
		}
		if e = m.writeKubeconfigs(b); e != nil {
			return e
		}
		if e = retry(ctx, 30, func() error { return m.captureInfrastructure(ctx) }); e != nil {
			return e
		}
	} else {
		if _, e = m.Shell(ctx, "sudo", "systemctl", "stop", "k3s"); e != nil {
			return e
		}
	}
	if e = m.CreateContext(ctx); e != nil {
		return e
	}
	m.SetProgress("Engine ready")
	return nil
}
func retry(ctx context.Context, n int, f func() error) error {
	var e error
	for i := 0; i < n; i++ {
		if e = f(); e == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return e
}
func (m *Manager) Stop(ctx context.Context) error {
	m.SetProgress("Stopping Kubernetes and Docker gracefully…")
	if _, e := m.Shell(ctx, "sudo", "systemctl", "stop", "k3s", "docker"); e != nil {
		return e
	}
	m.SetProgress("Shutting down the virtual machine…")
	_, e := m.Lima(ctx, "stop", "--tty=false", Instance)
	if e == nil {
		m.SetProgress("Engine stopped · your data is preserved")
	}
	return e
}
func (m *Manager) Docker(ctx context.Context, method, path string, body io.Reader) ([]byte, error) {
	req, e := http.NewRequestWithContext(ctx, method, "http://docker"+path, body)
	if e != nil {
		return nil, e
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	r, e := m.docker.Do(req)
	if e != nil {
		return nil, e
	}
	defer r.Body.Close()
	b, e := io.ReadAll(io.LimitReader(r.Body, 32<<20))
	if e != nil {
		return nil, e
	}
	if r.StatusCode >= 400 {
		return nil, fmt.Errorf("engine %d: %s", r.StatusCode, b)
	}
	return b, nil
}
func (m *Manager) Kubectl(ctx context.Context, args ...string) ([]byte, error) {
	return m.Command(ctx, "kubectl", append([]string{"--kubeconfig", m.Paths.RawKubeconfig(), "--request-timeout=15s"}, args...)...)
}
func (m *Manager) CreateContext(ctx context.Context) error {
	endpoint := "host=unix://" + m.Paths.GatewaySocket()
	if _, e := m.Command(ctx, "docker", "context", "inspect", "dockerd-local"); e != nil {
		_, e = m.Command(ctx, "docker", "context", "create", "dockerd-local", "--description", "DockerD local engine", "--docker", endpoint)
		return e
	}
	_, e := m.Command(ctx, "docker", "context", "update", "dockerd-local", "--docker", endpoint)
	return e
}
func (m *Manager) ActivateContext(ctx context.Context) error {
	_, e := m.Command(ctx, "docker", "context", "use", "dockerd-local")
	return e
}
func (m *Manager) writeKubeconfigs(b []byte) error {
	var v map[string]any
	if e := json.Unmarshal(b, &v); e != nil {
		return e
	}
	clusters := v["clusters"].([]any)
	for _, c := range clusters {
		c.(map[string]any)["cluster"].(map[string]any)["server"] = "https://127.0.0.1:16443"
	}
	raw, _ := json.MarshalIndent(v, "", "  ")
	if e := os.WriteFile(m.Paths.RawKubeconfig(), raw, 0600); e != nil {
		return e
	}
	// Gateway preserves the upstream TLS certificate; kubeconfig uses the same CA.
	for _, c := range clusters {
		c.(map[string]any)["name"] = "dockerd-local"
		c.(map[string]any)["cluster"].(map[string]any)["server"] = "https://127.0.0.1:16444"
	}
	for _, c := range v["contexts"].([]any) {
		c.(map[string]any)["name"] = "dockerd-local"
		c.(map[string]any)["context"].(map[string]any)["cluster"] = "dockerd-local"
	}
	v["current-context"] = "dockerd-local"
	raw, _ = json.MarshalIndent(v, "", "  ")
	return os.WriteFile(m.Paths.Kubeconfig(), raw, 0600)
}
func (m *Manager) KubeTLS() (client, server *tls.Config, err error) {
	b, e := os.ReadFile(m.Paths.RawKubeconfig())
	if e != nil {
		return nil, nil, e
	}
	var v struct {
		Clusters []struct {
			Cluster struct {
				CA string `json:"certificate-authority-data"`
			}
		}
		Users []struct {
			User struct {
				Cert string `json:"client-certificate-data"`
				Key  string `json:"client-key-data"`
			}
		}
	}
	if e = json.Unmarshal(b, &v); e != nil {
		return nil, nil, e
	}
	if len(v.Clusters) == 0 || len(v.Users) == 0 {
		return nil, nil, errors.New("missing Kubernetes credentials")
	}
	ca, e := base64.StdEncoding.DecodeString(v.Clusters[0].Cluster.CA)
	if e != nil {
		return nil, nil, e
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(ca) {
		return nil, nil, errors.New("invalid Kubernetes CA")
	}
	// Fetch the serving key through the VM's authenticated SSH channel, never over HTTP.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cert, e := m.Shell(ctx, "sudo", "cat", "/var/lib/rancher/k3s/server/tls/serving-kube-apiserver.crt")
	if e != nil {
		return nil, nil, e
	}
	key, e := m.Shell(ctx, "sudo", "cat", "/var/lib/rancher/k3s/server/tls/serving-kube-apiserver.key")
	if e != nil {
		return nil, nil, e
	}
	pair, e := tls.X509KeyPair(cert, key)
	if e != nil {
		return nil, nil, e
	}
	client = &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: pool}
	// Verify client certificates against K3s' client CA. Forward the identity using
	// the original client certificate on a per-request transport (see API server).
	clientCA, e := m.Shell(ctx, "sudo", "cat", "/var/lib/rancher/k3s/server/tls/client-ca.crt")
	if e != nil {
		return nil, nil, e
	}
	cp := x509.NewCertPool()
	if !cp.AppendCertsFromPEM(clientCA) {
		return nil, nil, errors.New("invalid client CA")
	}
	certData, _ := base64.StdEncoding.DecodeString(v.Users[0].User.Cert)
	keyData, _ := base64.StdEncoding.DecodeString(v.Users[0].User.Key)
	cc, e := tls.X509KeyPair(certData, keyData)
	if e != nil {
		return nil, nil, e
	}
	client.Certificates = []tls.Certificate{cc}
	server = &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{pair}, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: cp}
	// The gateway uses the app-owned administrator identity upstream. Accept only
	// that exact identity locally, never arbitrary certificates signed by K3s.
	server.VerifyConnection = func(state tls.ConnectionState) error {
		if len(state.PeerCertificates) == 0 || !bytes.Equal(state.PeerCertificates[0].Raw, cc.Certificate[0]) {
			return errors.New("use the DockerD kubeconfig to connect")
		}
		return nil
	}
	return client, server, nil
}

func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
func (m *Manager) Terminal(kind, id, namespace string) (string, error) {
	var args []string
	binary := m.Paths.Binary("docker")
	if kind == "pods" {
		binary = m.Paths.Binary("kubectl")
		args = []string{"--kubeconfig", m.Paths.Kubeconfig(), "-n", namespace, "exec", "-it", id, "--", "sh"}
	} else {
		args = []string{"--context", "dockerd-local", "exec", "-it", id, "sh"}
	}
	line := quote(binary)
	for _, a := range args {
		line += " " + quote(a)
	}
	dir := filepath.Join(m.Paths.Home, "terminals")
	if e := os.MkdirAll(dir, 0700); e != nil {
		return "", e
	}
	path := filepath.Join(dir, strconv.FormatInt(time.Now().UnixNano(), 10)+".command")
	return path, os.WriteFile(path, []byte("#!/bin/sh\n"+line+"\n"), 0700)
}
