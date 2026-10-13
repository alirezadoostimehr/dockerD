package runtime

import (
	"context"
	"dockerd.local/backend/internal/idle"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Metadata struct {
	Name              string            `json:"name"`
	Namespace         string            `json:"namespace"`
	UID               string            `json:"uid"`
	DeletionTimestamp *string           `json:"deletionTimestamp"`
	Labels            map[string]string `json:"labels"`
	Owners            []struct {
		UID  string `json:"uid"`
		Kind string `json:"kind"`
	} `json:"ownerReferences"`
}
type Pod struct {
	Metadata Metadata `json:"metadata"`
	Status   struct {
		Phase             string `json:"phase"`
		ContainerStatuses []struct {
			Ready        bool `json:"ready"`
			RestartCount int  `json:"restartCount"`
		} `json:"containerStatuses"`
	} `json:"status"`
	Spec struct {
		Containers []struct {
			Name  string `json:"name"`
			Image string `json:"image"`
		} `json:"containers"`
	} `json:"spec"`
}
type Container struct {
	ID     string            `json:"Id"`
	Names  []string          `json:"Names"`
	Image  string            `json:"Image"`
	State  string            `json:"State"`
	Status string            `json:"Status"`
	Labels map[string]string `json:"Labels"`
	Ports  []struct {
		PrivatePort int
		PublicPort  int
		Type        string
	} `json:"Ports"`
}

func (m *Manager) Containers(ctx context.Context) ([]Container, error) {
	b, e := m.Docker(ctx, "GET", "/containers/json?all=1", nil)
	if e != nil {
		return nil, e
	}
	var v []Container
	e = json.Unmarshal(b, &v)
	return v, e
}
func (m *Manager) Pods(ctx context.Context) ([]Pod, error) {
	b, e := m.Kubectl(ctx, "get", "pods", "-A", "-o", "json")
	if e != nil {
		return nil, e
	}
	var v struct{ Items []Pod }
	e = json.Unmarshal(b, &v)
	return v.Items, e
}
func (m *Manager) captureInfrastructure(ctx context.Context) error {
	path := filepath.Join(m.Paths.Home, "infrastructure.json")
	if b, e := os.ReadFile(path); e == nil {
		return json.Unmarshal(b, &m.infrastructure)
	}
	b, e := m.Kubectl(ctx, "-n", "kube-system", "get", "deployment", "coredns", "local-path-provisioner", "-o", "json")
	if e != nil {
		return e
	}
	var v struct{ Items []struct{ Metadata Metadata } }
	if e = json.Unmarshal(b, &v); e != nil {
		return e
	}
	if len(v.Items) != 2 {
		return fmt.Errorf("cannot establish system infrastructure identity")
	}
	for _, x := range v.Items {
		m.infrastructure[x.Metadata.UID] = true
	}
	b, _ = json.Marshal(m.infrastructure)
	return os.WriteFile(path, b, 0600)
}

func (m *Manager) LoadInfrastructure() error {
	b, e := os.ReadFile(filepath.Join(m.Paths.Home, "infrastructure.json"))
	if e != nil {
		return e
	}
	return json.Unmarshal(b, &m.infrastructure)
}
func (m *Manager) systemPods(ctx context.Context, pods []Pod) (map[string]bool, error) {
	b, e := m.Kubectl(ctx, "-n", "kube-system", "get", "replicasets", "-o", "json")
	if e != nil {
		return nil, e
	}
	var v struct{ Items []struct{ Metadata Metadata } }
	if e = json.Unmarshal(b, &v); e != nil {
		return nil, e
	}
	replicas := map[string]bool{}
	for _, rs := range v.Items {
		for _, o := range rs.Metadata.Owners {
			if o.Kind == "Deployment" && m.infrastructure[o.UID] {
				replicas[rs.Metadata.UID] = true
			}
		}
	}
	result := map[string]bool{}
	for _, p := range pods {
		if p.Metadata.Namespace != "kube-system" {
			continue
		}
		for _, o := range p.Metadata.Owners {
			if o.Kind == "ReplicaSet" && replicas[o.UID] {
				result[p.Metadata.UID] = true
			}
		}
	}
	return result, nil
}
func WorkloadBlockers(containers []Container, pods []Pod, system map[string]bool) []idle.Blocker {
	b := []idle.Blocker{}
	for _, c := range containers {
		if c.State != "running" && c.State != "paused" && c.State != "restarting" {
			continue
		}
		if system[c.Labels["io.kubernetes.pod.uid"]] {
			continue
		}
		name := c.ID
		if len(c.Names) > 0 {
			name = strings.TrimPrefix(c.Names[0], "/")
		}
		b = append(b, idle.Blocker{Kind: "container", Name: name, Detail: c.State + " · " + c.Image})
	}
	for _, p := range pods {
		if system[p.Metadata.UID] {
			continue
		}
		if p.Status.Phase == "Succeeded" || p.Status.Phase == "Failed" {
			continue
		}
		b = append(b, idle.Blocker{Kind: "pod", Name: p.Metadata.Namespace + "/" + p.Metadata.Name, Detail: p.Status.Phase})
	}
	return b
}

// Observe must be serialized by the control service; audit offsets are monotonic.
func (m *Manager) Observe(ctx context.Context, kubernetes bool) idle.Snapshot {
	s := idle.Snapshot{At: time.Now(), Healthy: true, Blockers: []idle.Blocker{}}
	fail := func(e error) {
		s.Healthy = false
		s.Blockers = append(s.Blockers, idle.Blocker{Kind: "monitor", Name: "Unable to verify activity", Detail: e.Error()})
	}
	containers, e := m.Containers(ctx)
	if e != nil {
		fail(e)
		return s
	}
	var pods []Pod
	system := map[string]bool{}
	if kubernetes {
		pods, e = m.Pods(ctx)
		if e != nil {
			fail(e)
			return s
		}
		system, e = m.systemPods(ctx, pods)
		if e != nil {
			fail(e)
			return s
		}
		if e = m.audit(ctx); e != nil {
			fail(e)
		}
		for _, b := range m.sessions {
			s.Blockers = append(s.Blockers, b)
		}
	}
	s.Blockers = append(s.Blockers, WorkloadBlockers(containers, pods, system)...)
	// An exec started directly against the engine can outlive its client connection.
	for _, c := range containers {
		if c.State != "running" {
			continue
		}
		raw, e := m.Docker(ctx, "GET", "/containers/"+c.ID+"/json", nil)
		if e != nil {
			fail(e)
			continue
		}
		var v struct{ ExecIDs []string }
		if e = json.Unmarshal(raw, &v); e != nil {
			fail(e)
			continue
		}
		for _, id := range v.ExecIDs {
			raw, e = m.Docker(ctx, "GET", "/exec/"+id+"/json", nil)
			if e != nil {
				fail(e)
				continue
			}
			var ex struct{ Running bool }
			if e = json.Unmarshal(raw, &ex); e != nil {
				fail(e)
				continue
			}
			if ex.Running {
				s.Blockers = append(s.Blockers, idle.Blocker{Kind: "exec", Name: "Container exec " + id[:min(12, len(id))], Detail: "An exec process is running."})
			}
		}
	}
	s.At = time.Now()
	return s
}
func (m *Manager) audit(ctx context.Context) error {
	// Reading from byte offsets avoids replay on every sample. Rotation invalidates
	// the baseline rather than guessing that lost sessions have completed.
	size, e := m.Shell(ctx, "sudo", "stat", "-c", "%s", "/var/log/dockerd/audit.log")
	if e != nil {
		return e
	}
	n, e := strconv.ParseInt(strings.TrimSpace(string(size)), 10, 64)
	if e != nil {
		return e
	}
	if n < m.auditOffset {
		return fmt.Errorf("Kubernetes audit log rotated; restart the engine to establish a fresh session baseline")
	}
	if n == m.auditOffset {
		return nil
	}
	if n-m.auditOffset > 32<<20 {
		return fmt.Errorf("audit backlog exceeds safe reconciliation limit; restart the engine")
	}
	b, e := m.Shell(ctx, "sudo", "tail", "-c", "+"+strconv.FormatInt(m.auditOffset+1, 10), "/var/log/dockerd/audit.log")
	if e != nil {
		return e
	}
	m.auditOffset += int64(len(b))
	lines := strings.Split(m.auditPartial+string(b), "\n")
	m.auditPartial = lines[len(lines)-1]
	for _, line := range lines[:len(lines)-1] {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var event struct {
			AuditID   string                                                  `json:"auditID"`
			Stage     string                                                  `json:"stage"`
			ObjectRef struct{ Resource, Subresource, Namespace, Name string } `json:"objectRef"`
		}
		if e = json.Unmarshal([]byte(line), &event); e != nil {
			return fmt.Errorf("invalid audit event: %w", e)
		}
		o := event.ObjectRef
		if o.Resource != "pods" {
			continue
		}
		switch o.Subresource {
		case "exec", "attach", "portforward":
		default:
			continue
		}
		switch event.Stage {
		case "RequestReceived", "ResponseStarted":
			m.sessions[event.AuditID] = idle.Blocker{Kind: o.Subresource, Name: o.Namespace + "/" + o.Name, Detail: "Kubernetes " + o.Subresource + " session"}
		case "ResponseComplete", "Panic":
			delete(m.sessions, event.AuditID)
		}
	}
	return nil
}
func (m *Manager) ResetAudit() {
	m.auditOffset = 0
	m.auditPartial = ""
	m.sessions = map[string]idle.Blocker{}
}
