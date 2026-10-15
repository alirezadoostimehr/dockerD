package runtime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dockerd.local/backend/internal/config"
)

// Opt-in because this creates and boots a real, isolated VM. It never uses the
// application's VM or Docker configuration. Run from backend with:
// DOCKERD_LIVE_TEST=1 go test -v ./internal/runtime -run TestSocketAccessLive -timeout 20m
func TestSocketAccessLive(t *testing.T) {
	if os.Getenv("DOCKERD_LIVE_TEST") != "1" {
		t.Skip("set DOCKERD_LIVE_TEST=1 to create a dedicated test VM")
	}
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	home, err := os.MkdirTemp(filepath.Join(root, ".runtime"), "socket-")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("DOCKER_CONFIG", filepath.Join(home, "docker-config"))
	m := New(Paths{Home: home, Bundle: filepath.Join(root, ".runtime/bundle"), Resources: filepath.Join(root, "resources")})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		if m.Running(ctx) {
			if _, err := m.Lima(ctx, "stop", "--tty=false", Instance); err != nil {
				t.Errorf("stop test VM (retained at %s): %v", home, err)
				return
			}
		}
		if m.Exists() {
			if _, err := m.Lima(ctx, "delete", "--force", Instance); err != nil {
				t.Errorf("delete test VM (retained at %s): %v", home, err)
				return
			}
		}
		os.RemoveAll(home)
	})
	// Reuse only a verified image cache, never another VM's disk or credentials.
	if seed := os.Getenv("DOCKERD_TEST_IMAGE"); seed != "" {
		if err := os.MkdirAll(filepath.Join(home, "images"), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.Link(seed, filepath.Join(home, "images/linux.img")); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	cfg := config.Defaults()
	cfg.Kubernetes = false
	cfg.CPUs, cfg.MemoryGiB = 2, 2
	shell := func(args ...string) string {
		t.Helper()
		out, err := m.Shell(ctx, args...)
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(string(out))
	}
	ping := func() {
		t.Helper()
		m.docker.CloseIdleConnections()
		out, err := m.Docker(ctx, "GET", "/_ping", nil)
		if err != nil || string(out) != "OK" {
			t.Fatalf("forwarded Docker ping: %q, %v", out, err)
		}
	}
	if err := m.Start(ctx, cfg); err != nil {
		log, _ := os.ReadFile(filepath.Join(home, "runtime.log"))
		t.Fatalf("first boot: %v\n%s", err, log)
	}
	ping()
	uid := shell("id", "-u")
	if got := shell("stat", "-c", "%u %a", "/var/run/docker.sock"); got != uid+" 660" {
		t.Fatalf("socket ownership/mode: %s", got)
	}
	t.Log("PASS first boot through the original SSH connection")

	// Recreate the old bug in this VM: the persistent SSH connection still lacks
	// the group that provisioning added. Root owns the socket and access fails.
	shell("sudo", "rm", "/etc/systemd/system/docker.service.d/10-dockerd-socket.conf")
	shell("sudo", "systemctl", "daemon-reload")
	shell("sudo", "chown", "root:docker", "/var/run/docker.sock")
	m.docker.CloseIdleConnections()
	if _, err := m.Docker(ctx, "GET", "/_ping", nil); err == nil {
		t.Fatal("failed to reproduce stale SSH group membership")
	}
	before := shell("systemctl", "show", "docker", "--property=MainPID", "--value")
	if err := m.ensureDockerSocketAccess(ctx); err != nil {
		t.Fatal(err)
	}
	ping()
	if after := shell("systemctl", "show", "docker", "--property=MainPID", "--value"); before != after {
		t.Fatal("repair unexpectedly restarted Docker")
	}
	t.Log("PASS existing VM repaired without restarting Docker")

	shell("sudo", "systemctl", "restart", "docker")
	ping()
	t.Log("PASS socket ownership survives Docker restart")
	if err := m.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if err := m.Start(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	ping()
	t.Log("PASS VM stop/start")
}
