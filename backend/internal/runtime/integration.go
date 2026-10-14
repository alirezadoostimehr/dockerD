package runtime

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// InstallCLI is an explicit user action. Existing tools are never replaced.
func (m *Manager) InstallCLI() error {
	home, e := os.UserHomeDir()
	if e != nil {
		return e
	}
	dockerConfig := os.Getenv("DOCKER_CONFIG")
	if dockerConfig == "" {
		dockerConfig = filepath.Join(home, ".docker")
	}
	links := map[string]string{
		filepath.Join(home, ".local", "bin", "docker"):               m.Paths.Binary("docker"),
		filepath.Join(home, ".local", "bin", "kubectl"):              m.Paths.Binary("kubectl"),
		filepath.Join(dockerConfig, "cli-plugins", "docker-compose"): filepath.Join(m.Paths.Bundle, "cli-plugins", "docker-compose"),
		filepath.Join(dockerConfig, "cli-plugins", "docker-buildx"):  filepath.Join(m.Paths.Bundle, "cli-plugins", "docker-buildx"),
	}
	for link, target := range links {
		if _, e = os.Lstat(link); e == nil {
			current, e := os.Readlink(link)
			if e != nil || current != target {
				return fmt.Errorf("%s already exists; no tools were replaced", link)
			}
		} else if !os.IsNotExist(e) {
			return e
		}
		if _, e = os.Stat(target); e != nil {
			return e
		}
	}
	created := []string{}
	for link, target := range links {
		if _, e = os.Lstat(link); e == nil {
			continue
		}
		if e = os.MkdirAll(filepath.Dir(link), 0700); e == nil {
			e = os.Symlink(target, link)
		}
		if e != nil {
			for _, p := range created {
				_ = os.Remove(p)
			}
			return e
		}
		created = append(created, link)
	}
	return nil
}

func (m *Manager) Logs(ctx context.Context, kind, id, namespace string) ([]byte, error) {
	var args []string
	binary := m.Paths.Binary("docker")
	if kind == "pods" {
		binary = m.Paths.Binary("kubectl")
		args = []string{"--kubeconfig", m.Paths.Kubeconfig(), "--request-timeout=15s", "-n", namespace, "logs", id, "--tail=300", "--all-containers=true"}
	} else {
		args = []string{"--host", "unix://" + m.Paths.GatewaySocket(), "logs", "--tail", "300", id}
	}
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Env = m.Env()
	b, e := cmd.CombinedOutput()
	if e != nil {
		return nil, fmt.Errorf("logs: %w: %s", e, b)
	}
	return b, nil
}

type Metrics struct {
	CPUPercent     float64   `json:"cpuPercent"`
	MemoryUsedGiB  float64   `json:"memoryUsedGiB"`
	MemoryTotalGiB float64   `json:"memoryTotalGiB"`
	DiskUsedGiB    float64   `json:"diskUsedGiB"`
	DiskTotalGiB   float64   `json:"diskTotalGiB"`
	At             time.Time `json:"at"`
}

func (m *Manager) Metrics() *Metrics {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.metrics == nil {
		return nil
	}
	v := *m.metrics
	return &v
}
func (m *Manager) sampleMetrics(ctx context.Context) {
	b, e := m.Shell(ctx, "sh", "-c", "head -n 1 /proc/stat; cat /proc/meminfo; df -B1 --output=used,size / | tail -n 1")
	if e != nil {
		return
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) < 3 {
		return
	}
	cpu := strings.Fields(lines[0])
	if len(cpu) < 9 {
		return
	}
	var total, idle uint64
	for i := 1; i <= 8; i++ {
		v, e := strconv.ParseUint(cpu[i], 10, 64)
		if e != nil {
			return
		}
		total += v
		if i == 4 || i == 5 {
			idle += v
		}
	}
	mem := map[string]float64{}
	for _, line := range lines[1 : len(lines)-1] {
		parts := strings.Fields(line)
		if len(parts) > 1 {
			mem[parts[0]], _ = strconv.ParseFloat(parts[1], 64)
		}
	}
	disk := strings.Fields(lines[len(lines)-1])
	if len(disk) != 2 {
		return
	}
	used, _ := strconv.ParseFloat(disk[0], 64)
	capacity, _ := strconv.ParseFloat(disk[1], 64)
	m.mu.Lock()
	defer m.mu.Unlock()
	percent := float64(0)
	if m.cpuTotal != 0 && total > m.cpuTotal && idle >= m.cpuIdle {
		percent = 100 * (1 - float64(idle-m.cpuIdle)/float64(total-m.cpuTotal))
	}
	m.cpuTotal = total
	m.cpuIdle = idle
	m.metrics = &Metrics{CPUPercent: percent, MemoryUsedGiB: (mem["MemTotal:"] - mem["MemAvailable:"]) / (1024 * 1024), MemoryTotalGiB: mem["MemTotal:"] / (1024 * 1024), DiskUsedGiB: used / (1 << 30), DiskTotalGiB: capacity / (1 << 30), At: time.Now()}
}
