package runtime

import (
	"dockerd.local/backend/internal/config"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Artifact struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
}
type Manifest struct {
	Guest struct{ Docker, K3s, Image Artifact } `json:"guest"`
}

func (m *Manager) Template(s config.Settings) ([]byte, error) {
	b, e := os.ReadFile(filepath.Join(m.Paths.Resources, "runtime-lock.json"))
	if e != nil {
		return nil, e
	}
	var lock Manifest
	if e = json.Unmarshal(b, &lock); e != nil {
		return nil, e
	}
	script := `#!/bin/bash
set -euo pipefail
export DEBIAN_FRONTEND=noninteractive
if [ ! -f /var/lib/dockerd-provisioned ]; then
  apt-get update
  apt-get install -y ca-certificates curl iptables conntrack socat
  curl --fail --location --retry 3 DOCKER_URL -o /tmp/docker.tgz
  echo 'DOCKER_HASH  /tmp/docker.tgz' | sha256sum -c -
  tar -xzf /tmp/docker.tgz -C /usr/local/bin --strip-components=1
  rm /tmp/docker.tgz
  groupadd -f docker
  usermod -aG docker "${LIMA_CIDATA_USER}"
  mkdir -p /etc/docker /etc/rancher/k3s /var/log/dockerd
  echo '{"log-driver":"local"}' > /etc/docker/daemon.json
  cat > /etc/systemd/system/docker.service <<'UNIT'
[Unit]
Description=DockerD container engine
After=network-online.target
Wants=network-online.target
[Service]
Type=notify
ExecStart=/usr/local/bin/dockerd --host=unix:///var/run/docker.sock
Restart=on-failure
TimeoutStartSec=0
TimeoutStopSec=120
Delegate=yes
KillMode=process
LimitNOFILE=infinity
TasksMax=infinity
[Install]
WantedBy=multi-user.target
UNIT
  curl --fail --location --retry 3 K3S_URL -o /usr/local/bin/k3s
  echo 'K3S_HASH  /usr/local/bin/k3s' | sha256sum -c -
  chmod 755 /usr/local/bin/k3s
  cat > /etc/rancher/k3s/audit.yaml <<'AUDIT'
apiVersion: audit.k8s.io/v1
kind: Policy
rules:
  - level: Metadata
    resources:
      - group: ""
        resources: ["pods/exec", "pods/attach", "pods/portforward"]
  - level: None
AUDIT
  cat > /etc/systemd/system/k3s.service <<'UNIT'
[Unit]
Description=DockerD Kubernetes
After=docker.service
Requires=docker.service
[Service]
Type=notify
ExecStart=/usr/local/bin/k3s server --docker --disable traefik --disable servicelb --disable metrics-server --write-kubeconfig-mode 600 --tls-san 127.0.0.1 --kube-apiserver-arg audit-policy-file=/etc/rancher/k3s/audit.yaml --kube-apiserver-arg audit-log-path=/var/log/dockerd/audit.log --kube-apiserver-arg audit-log-mode=blocking --kube-apiserver-arg audit-log-maxsize=100 --kube-apiserver-arg audit-log-maxbackup=2
Restart=on-failure
TimeoutStartSec=0
TimeoutStopSec=120
KillMode=control-group
Delegate=yes
LimitNOFILE=1048576
TasksMax=infinity
[Install]
WantedBy=multi-user.target
UNIT
  echo 'net.ipv4.ip_forward=1' > /etc/sysctl.d/99-dockerd.conf
  sysctl -p /etc/sysctl.d/99-dockerd.conf
  systemctl daemon-reload
  systemctl enable docker
  touch /var/lib/dockerd-provisioned
fi
systemctl start docker
`
	script = strings.NewReplacer("DOCKER_URL", quote(lock.Guest.Docker.URL), "DOCKER_HASH", lock.Guest.Docker.SHA256, "K3S_URL", quote(lock.Guest.K3s.URL), "K3S_HASH", lock.Guest.K3s.SHA256).Replace(script)
	mounts := []map[string]any{}
	for _, p := range s.SharedFolders {
		mounts = append(mounts, map[string]any{"location": p, "writable": true})
	}
	// JSON is a YAML subset; encoding avoids interpolation of user folder names.
	template := map[string]any{
		"minimumLimaVersion": "2.0.0", "vmType": "vz", "arch": "aarch64", "cpus": s.CPUs, "memory": fmt.Sprintf("%dGiB", s.MemoryGiB), "disk": fmt.Sprintf("%dGiB", s.DiskGiB),
		"images":    []any{map[string]any{"location": lock.Guest.Image.URL, "arch": "aarch64", "digest": "sha256:" + lock.Guest.Image.SHA256}},
		"mountType": "virtiofs", "mounts": mounts, "containerd": map[string]bool{"system": false, "user": false},
		"hostResolver": map[string]any{"hosts": map[string]string{"host.docker.internal": "host.lima.internal"}},
		"provision":    []any{map[string]any{"mode": "system", "script": script}},
		"portForwards": []any{
			map[string]any{"guestSocket": "/var/run/docker.sock", "hostSocket": "{{.Dir}}/sock/docker.sock"},
			map[string]any{"guestPort": 6443, "hostPort": 16443, "hostIP": "127.0.0.1"},
			map[string]any{"guestPortRange": []int{1, 65535}, "hostIP": "127.0.0.1"},
		},
	}
	return json.MarshalIndent(template, "", "  ")
}
