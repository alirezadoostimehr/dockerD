package runtime

import (
	"context"
	"fmt"
)

// Give the dedicated VM user ownership of the socket. Lima establishes its SSH
// forwarding session before provisioning adds supplementary groups, so relying
// on membership in the docker group breaks the first boot. ExecStartPost keeps
// ownership correct after daemon restarts; the final chown also repairs an
// already-running VM created by an older app without restarting its workloads.
const dockerSocketAccessScript = `set -eu
socket_uid=${DOCKERD_SOCKET_UID:-${SUDO_UID:-}}
case "$socket_uid" in
  ''|*[!0-9]*) echo 'Cannot determine the Docker socket owner' >&2; exit 1 ;;
esac
mkdir -p /etc/systemd/system/docker.service.d
printf '[Service]\nExecStartPost=/bin/chown %s /var/run/docker.sock\n' "$socket_uid" > /etc/systemd/system/docker.service.d/10-dockerd-socket.conf
systemctl daemon-reload
systemctl start docker
chown "$socket_uid" /var/run/docker.sock
`

func (m *Manager) ensureDockerSocketAccess(ctx context.Context) error {
	if _, err := m.Shell(ctx, "sudo", "sh", "-c", dockerSocketAccessScript); err != nil {
		return fmt.Errorf("configure Docker socket access: %w", err)
	}
	return nil
}
