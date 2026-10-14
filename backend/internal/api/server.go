package api

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"dockerd.local/backend/internal/config"
	"dockerd.local/backend/internal/gateway"
	"dockerd.local/backend/internal/idle"
	engine "dockerd.local/backend/internal/runtime"
)

type Status struct {
	State            string          `json:"state"`
	Message          string          `json:"message"`
	Error            string          `json:"error,omitempty"`
	RuntimeInstalled bool            `json:"runtimeInstalled"`
	VMExists         bool            `json:"vmExists"`
	Settings         config.Settings `json:"settings"`
	Activity         idle.Decision   `json:"activity"`
	Home             string          `json:"home"`
	Kubeconfig       string          `json:"kubeconfig"`
	Metrics          *engine.Metrics `json:"metrics,omitempty"`
}
type Forward struct {
	ID         string `json:"id"`
	Pod        string `json:"pod"`
	Namespace  string `json:"namespace"`
	LocalPort  int    `json:"localPort"`
	RemotePort int    `json:"remotePort"`
	State      string `json:"state"`
	Error      string `json:"error,omitempty"`
	cmd        *exec.Cmd
	cancel     context.CancelFunc
}
type Server struct {
	Manager            *engine.Manager
	Tracker            *gateway.Tracker
	mu                 sync.Mutex
	lifecycle          sync.Mutex
	settings           config.Settings
	state, lastError   string
	decision           idle.Decision
	policy             idle.Policy
	kubeServer         *http.Server
	forwards           map[string]*Forward
	eventHealthy       bool
	eventCancel        context.CancelFunc
	observedGeneration uint64
}

func New(m *engine.Manager) (*Server, error) {
	s, e := config.Load(filepath.Join(m.Paths.Home, "settings.json"))
	if e != nil {
		return nil, e
	}
	a := &Server{Manager: m, Tracker: gateway.NewTracker(), settings: s, state: "stopped", forwards: map[string]*Forward{}, decision: idle.Decision{State: "stopped", Blockers: []idle.Blocker{}}}
	a.configurePolicy()
	return a, nil
}
func (s *Server) configurePolicy() {
	s.policy.Enabled = s.settings.IdleEnabled
	s.policy.Timeout = time.Duration(s.settings.IdleMinutes) * time.Minute
	s.policy.Countdown = time.Duration(s.settings.CountdownSeconds) * time.Second
	s.policy.MaxAge = 25 * time.Second
	s.policy.Reset()
}
func (s *Server) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.state
	if state == "active" && s.decision.State == "idle countdown" {
		state = s.decision.State
	}
	return Status{State: state, Message: s.Manager.Progress(), Error: s.lastError, RuntimeInstalled: s.Manager.Installed(), VMExists: s.Manager.Exists(), Settings: s.settings, Activity: s.decision, Home: s.Manager.Paths.Home, Kubeconfig: s.Manager.Paths.Kubeconfig(), Metrics: s.Manager.Metrics()}
}
func (s *Server) fail(e error) {
	s.mu.Lock()
	s.state = "error"
	s.lastError = e.Error()
	s.policy.Reset()
	s.mu.Unlock()
}
func (s *Server) ready() {
	s.mu.Lock()
	s.state = "active"
	s.lastError = ""
	s.policy.Reset()
	s.mu.Unlock()
	s.Tracker.Gate(false)
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/status", func(w http.ResponseWriter, r *http.Request) { respond(w, s.Status()) })
	mux.HandleFunc("POST /v1/start", s.start)
	mux.HandleFunc("POST /v1/stop", s.stop)
	mux.HandleFunc("POST /v1/idle/reset", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.policy.Reset()
		s.decision.Deadline = nil
		s.decision.State = "active"
		s.mu.Unlock()
		respond(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("PUT /v1/settings", s.saveSettings)
	mux.HandleFunc("POST /v1/context/activate", func(w http.ResponseWriter, r *http.Request) {
		if e := s.Manager.ActivateContext(r.Context()); e != nil {
			failure(w, e)
			return
		}
		respond(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("GET /v1/resources/{kind}", s.resources)
	mux.HandleFunc("POST /v1/cli/install", func(w http.ResponseWriter, r *http.Request) {
		if e := s.Manager.InstallCLI(); e != nil {
			failure(w, e)
			return
		}
		respond(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("GET /v1/detail", s.detail)
	mux.HandleFunc("POST /v1/action", s.action)
	mux.HandleFunc("GET /v1/forwards", s.listForwards)
	mux.HandleFunc("POST /v1/forwards", s.createForward)
	mux.HandleFunc("DELETE /v1/forwards/{id}", s.deleteForward)
	mux.HandleFunc("GET /v1/diagnostics", func(w http.ResponseWriter, r *http.Request) {
		b, e := os.ReadFile(filepath.Join(s.Manager.Paths.Home, "runtime.log"))
		if e != nil && !errors.Is(e, os.ErrNotExist) {
			failure(w, e)
			return
		}
		if len(b) > 64000 {
			b = b[len(b)-64000:]
		}
		respond(w, map[string]string{"text": string(b)})
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		mux.ServeHTTP(w, r)
	})
}
func decode(r *http.Request, v any) error {
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	return d.Decode(v)
}
func respond(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
func failure(w http.ResponseWriter, e error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(400)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": e.Error()})
}
func (s *Server) start(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	if s.state == "starting" || s.state == "stopping" || s.state == "active" {
		s.mu.Unlock()
		failure(w, errors.New("engine is already running or changing state"))
		return
	}
	s.state = "starting"
	s.lastError = ""
	settings := s.settings
	s.mu.Unlock()
	go func() {
		s.lifecycle.Lock()
		defer s.lifecycle.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		if e := s.Manager.Start(ctx, settings); e != nil {
			s.fail(e)
			return
		}
		s.Manager.ResetAudit()
		if settings.Kubernetes {
			if e := s.startKubeGateway(); e != nil {
				s.fail(e)
				return
			}
		}
		s.startEvents()
		s.ready()
	}()
	respond(w, map[string]bool{"accepted": true})
}
func (s *Server) stop(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	if s.state != "active" && s.state != "error" {
		s.mu.Unlock()
		failure(w, errors.New("engine is not running"))
		return
	}
	s.state = "stopping"
	s.policy.Reset()
	s.mu.Unlock()
	s.Tracker.Gate(true)
	go func() { s.lifecycle.Lock(); defer s.lifecycle.Unlock(); s.stopEngine() }()
	respond(w, map[string]bool{"accepted": true})
}
func (s *Server) stopEngine() {
	s.mu.Lock()
	for _, f := range s.forwards {
		if f.cancel != nil {
			f.cancel()
		}
	}
	s.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if e := s.Manager.Stop(ctx); e != nil {
		s.fail(e)
		return
	}
	if s.eventCancel != nil {
		s.eventCancel()
		s.eventCancel = nil
	}
	if s.kubeServer != nil {
		_ = s.kubeServer.Close()
		s.kubeServer = nil
	}
	s.mu.Lock()
	s.state = "stopped"
	s.lastError = ""
	s.decision = idle.Decision{State: "stopped", Blockers: []idle.Blocker{}}
	s.policy.Reset()
	s.mu.Unlock()
}
func (s *Server) saveSettings(w http.ResponseWriter, r *http.Request) {
	var cfg config.Settings
	if e := decode(r, &cfg); e != nil {
		failure(w, e)
		return
	}
	if e := cfg.Validate(); e != nil {
		failure(w, e)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	runtimeChange := cfg.CPUs != s.settings.CPUs || cfg.MemoryGiB != s.settings.MemoryGiB || cfg.Kubernetes != s.settings.Kubernetes || strings.Join(cfg.SharedFolders, "\x00") != strings.Join(s.settings.SharedFolders, "\x00")
	if cfg.DiskGiB != s.settings.DiskGiB && s.Manager.Exists() {
		failure(w, errors.New("disk capacity cannot be changed after VM creation"))
		return
	}
	if runtimeChange && s.state != "stopped" {
		failure(w, errors.New("stop the engine before changing CPU, memory, Kubernetes, or shared folders"))
		return
	}
	if e := config.Save(filepath.Join(s.Manager.Paths.Home, "settings.json"), cfg); e != nil {
		failure(w, e)
		return
	}
	s.settings = cfg
	s.configurePolicy()
	respond(w, cfg)
}
func (s *Server) startKubeGateway() error {
	if s.kubeServer != nil {
		_ = s.kubeServer.Close()
		s.kubeServer = nil
	}
	clientTLS, serverTLS, e := s.Manager.KubeTLS()
	if e != nil {
		return e
	}
	target, _ := url.Parse("https://127.0.0.1:16443")
	transport := &http.Transport{TLSClientConfig: clientTLS, ForceAttemptHTTP2: false, ResponseHeaderTimeout: 30 * time.Second}
	listener, e := net.Listen("tcp", "127.0.0.1:16444")
	if e != nil {
		return fmt.Errorf("Kubernetes gateway port 16444: %w", e)
	}
	s.kubeServer = &http.Server{Handler: gateway.Proxy(target, transport, s.Tracker, "Kubernetes"), ReadHeaderTimeout: 10 * time.Second, TLSConfig: serverTLS}
	server := s.kubeServer
	go func() { _ = server.Serve(tls.NewListener(listener, serverTLS)) }()
	return nil
}
func (s *Server) startEvents() {
	if s.eventCancel != nil {
		s.eventCancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.eventCancel = cancel
	go func() {
		for ctx.Err() == nil {
			req, _ := http.NewRequestWithContext(ctx, "GET", "http://docker/events", nil)
			client := &http.Client{Transport: gateway.UnixTransport(s.Manager.Paths.DockerSocket())}
			response, e := client.Do(req)
			if e == nil && response.StatusCode == 200 {
				s.mu.Lock()
				s.eventHealthy = true
				s.mu.Unlock()
				decoder := json.NewDecoder(response.Body)
				for {
					var event struct{ Action string }
					if decoder.Decode(&event) != nil {
						break
					}
					if event.Action != "" && !strings.HasPrefix(event.Action, "health_status") {
						s.mu.Lock()
						s.policy.Reset()
						s.mu.Unlock()
					}
				}
				response.Body.Close()
			} else if response != nil {
				response.Body.Close()
			}
			s.mu.Lock()
			s.eventHealthy = false
			s.policy.Reset()
			s.mu.Unlock()
			select {
			case <-ctx.Done():
				return
			case <-time.After(3 * time.Second):
			}
		}
	}()
}
func (s *Server) RunMonitor(ctx context.Context) {
	// Recover a running VM after the UI or service has restarted.
	s.lifecycle.Lock()
	probe, cancel := context.WithTimeout(ctx, 20*time.Second)
	if s.Manager.Running(probe) {
		s.mu.Lock()
		cfg := s.settings
		s.mu.Unlock()
		var e error
		if cfg.Kubernetes {
			e = s.Manager.LoadInfrastructure()
			if e == nil {
				e = s.startKubeGateway()
			}
		}
		if e != nil {
			s.fail(e)
		} else {
			s.startEvents()
			s.ready()
		}
	}
	cancel()
	s.lifecycle.Unlock()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.tick(ctx)
		}
	}
}
func (s *Server) tick(ctx context.Context) {
	s.lifecycle.Lock()
	defer s.lifecycle.Unlock()
	s.mu.Lock()
	if s.state != "active" {
		s.mu.Unlock()
		return
	}
	cfg := s.settings
	s.mu.Unlock()
	_, generation := s.Tracker.Snapshot()
	probe, cancel := context.WithTimeout(ctx, 20*time.Second)
	snapshot := s.Manager.Observe(probe, cfg.Kubernetes)
	cancel()
	tracked, after := s.Tracker.Snapshot()
	snapshot.Blockers = append(snapshot.Blockers, tracked...)
	s.mu.Lock()
	if !s.eventHealthy {
		snapshot.Healthy = false
	}
	if after != generation || generation != s.observedGeneration {
		s.policy.Reset()
	}
	s.observedGeneration = after
	decision := s.policy.Evaluate(time.Now(), snapshot)
	s.decision = decision
	state := s.state
	s.mu.Unlock()
	if !decision.ShouldStop || state != "active" {
		return
	}
	if !s.Tracker.TryGate(after) {
		return
	}
	// Once admission is closed, get a final observation before committing to stop.
	probe, cancel = context.WithTimeout(ctx, 20*time.Second)
	final := s.Manager.Observe(probe, cfg.Kubernetes)
	cancel()
	s.mu.Lock()
	healthy := s.eventHealthy
	stillActive := s.state == "active"
	// A concurrent Keep running action or settings change invalidates the old
	// deadline. Re-evaluate while holding the state lock before committing.
	finalDecision := s.policy.Evaluate(time.Now(), final)
	if !final.Healthy || !healthy || len(final.Blockers) > 0 || !stillActive || !finalDecision.ShouldStop {
		s.policy.Reset()
		s.mu.Unlock()
		s.Tracker.Gate(!stillActive)
		return
	}
	s.state = "stopping"
	s.mu.Unlock()
	s.stopEngine()
}

type Resource struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Subtitle  string `json:"subtitle"`
	State     string `json:"state"`
	Group     string `json:"group"`
	Namespace string `json:"namespace"`
	Size      int64  `json:"size"`
	Ports     string `json:"ports"`
}

func (s *Server) resources(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	kind := r.PathValue("kind")
	out := []Resource{}
	switch kind {
	case "containers":
		v, e := s.Manager.Containers(ctx)
		if e != nil {
			failure(w, e)
			return
		}
		for _, c := range v {
			name := c.ID
			if len(c.Names) > 0 {
				name = strings.TrimPrefix(c.Names[0], "/")
			}
			ports := []string{}
			for _, p := range c.Ports {
				if p.PublicPort > 0 {
					ports = append(ports, fmt.Sprintf("%d → %d/%s", p.PublicPort, p.PrivatePort, p.Type))
				}
			}
			out = append(out, Resource{ID: c.ID, Name: name, Subtitle: c.Image, State: c.State, Group: c.Labels["com.docker.compose.project"], Ports: strings.Join(ports, ", ")})
		}
	case "images":
		b, e := s.Manager.Docker(ctx, "GET", "/images/json", nil)
		if e != nil {
			failure(w, e)
			return
		}
		var v []struct {
			ID       string `json:"Id"`
			RepoTags []string
			Size     int64
		}
		if e = json.Unmarshal(b, &v); e != nil {
			failure(w, e)
			return
		}
		for _, x := range v {
			name := "<untagged>"
			if len(x.RepoTags) > 0 {
				name = x.RepoTags[0]
			}
			out = append(out, Resource{ID: x.ID, Name: name, Subtitle: x.ID, Size: x.Size, State: "available"})
		}
	case "volumes":
		b, e := s.Manager.Docker(ctx, "GET", "/volumes", nil)
		if e != nil {
			failure(w, e)
			return
		}
		var v struct {
			Volumes []struct{ Name, Driver, Mountpoint string }
		}
		if e = json.Unmarshal(b, &v); e != nil {
			failure(w, e)
			return
		}
		for _, x := range v.Volumes {
			out = append(out, Resource{ID: x.Name, Name: x.Name, Subtitle: x.Driver, State: "available"})
		}
	case "pods":
		v, e := s.Manager.Pods(ctx)
		if e != nil {
			failure(w, e)
			return
		}
		for _, p := range v {
			images := []string{}
			for _, c := range p.Spec.Containers {
				images = append(images, c.Image)
			}
			out = append(out, Resource{ID: p.Metadata.Name, Name: p.Metadata.Name, Namespace: p.Metadata.Namespace, Subtitle: strings.Join(images, ", "), State: p.Status.Phase, Group: p.Metadata.Namespace})
		}
	default:
		failure(w, errors.New("unknown resource kind"))
		return
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	respond(w, out)
}
func (s *Server) detail(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	kind, id, view, ns := q.Get("kind"), q.Get("id"), q.Get("view"), q.Get("namespace")
	if id == "" || strings.HasPrefix(id, "-") {
		failure(w, errors.New("invalid resource identifier"))
		return
	}
	var b []byte
	var e error
	if kind == "pods" {
		if view == "logs" {
			b, e = s.Manager.Logs(r.Context(), "pods", id, ns)
		} else {
			b, e = s.Manager.Kubectl(r.Context(), "-n", ns, "get", "pod", id, "-o", "json")
		}
	} else {
		switch kind {
		case "containers":
			if view == "logs" {
				b, e = s.Manager.Logs(r.Context(), "containers", id, "")
			} else {
				b, e = s.Manager.Docker(r.Context(), "GET", "/containers/"+url.PathEscape(id)+"/json", nil)
			}
		case "images":
			b, e = s.Manager.Docker(r.Context(), "GET", "/images/"+url.PathEscape(id)+"/json", nil)
		case "volumes":
			b, e = s.Manager.Docker(r.Context(), "GET", "/volumes/"+url.PathEscape(id), nil)
		default:
			e = errors.New("unknown resource kind")
		}
	}
	if e != nil {
		failure(w, e)
		return
	}
	respond(w, map[string]string{"text": string(b)})
}

type Action struct {
	Kind          string `json:"kind"`
	ID            string `json:"id"`
	Namespace     string `json:"namespace"`
	Action        string `json:"action"`
	Value         string `json:"value"`
	LocalPort     int    `json:"localPort"`
	ContainerPort int    `json:"containerPort"`
}

func (s *Server) action(w http.ResponseWriter, r *http.Request) {
	var a Action
	if e := decode(r, &a); e != nil {
		failure(w, e)
		return
	}
	if strings.HasPrefix(a.ID, "-") || strings.HasPrefix(a.Value, "-") {
		failure(w, errors.New("identifiers must not begin with '-'"))
		return
	}
	done, ok := s.Tracker.Admit(idle.Blocker{Kind: "operation", Name: a.Action + " " + a.ID, Detail: "Requested from DockerD"})
	if !ok {
		failure(w, errors.New("start the engine first"))
		return
	}
	defer done()
	ctx := r.Context()
	var e error
	var result []byte
	switch a.Action {
	case "run":
		if a.ID == "" || a.LocalPort < 0 || a.LocalPort > 65535 || a.ContainerPort < 0 || a.ContainerPort > 65535 || (a.LocalPort == 0) != (a.ContainerPort == 0) {
			e = errors.New("select an image and valid optional ports")
			break
		}
		body := map[string]any{"Image": a.ID}
		if a.LocalPort != 0 {
			port := strconv.Itoa(a.ContainerPort) + "/tcp"
			body["ExposedPorts"] = map[string]any{port: map[string]any{}}
			body["HostConfig"] = map[string]any{"PortBindings": map[string]any{port: []any{map[string]string{"HostIp": "127.0.0.1", "HostPort": strconv.Itoa(a.LocalPort)}}}}
		}
		encoded, _ := json.Marshal(body)
		path := "/containers/create"
		if a.Value != "" {
			path += "?name=" + url.QueryEscape(a.Value)
		}
		result, e = s.Manager.Docker(ctx, "POST", path, strings.NewReader(string(encoded)))
		if e != nil {
			break
		}
		var created struct {
			ID string `json:"Id"`
		}
		if e = json.Unmarshal(result, &created); e != nil {
			break
		}
		result, e = s.Manager.Docker(ctx, "POST", "/containers/"+created.ID+"/start", nil)
	case "terminal":
		if a.Kind != "containers" && a.Kind != "pods" {
			e = errors.New("terminal requires a container or pod")
			break
		}
		var path string
		path, e = s.Manager.Terminal(a.Kind, a.ID, a.Namespace)
		if e == nil {
			respond(w, map[string]string{"path": path})
			return
		}
	case "start", "stop", "restart":
		if a.Kind != "containers" {
			e = errors.New("action requires a container")
			break
		}
		result, e = s.Manager.Docker(ctx, "POST", "/containers/"+url.PathEscape(a.ID)+"/"+a.Action+"?t=20", nil)
	case "remove":
		switch a.Kind {
		case "containers":
			result, e = s.Manager.Docker(ctx, "DELETE", "/containers/"+url.PathEscape(a.ID), nil)
		case "images":
			result, e = s.Manager.Docker(ctx, "DELETE", "/images/"+url.PathEscape(a.ID), nil)
		case "volumes":
			result, e = s.Manager.Docker(ctx, "DELETE", "/volumes/"+url.PathEscape(a.ID), nil)
		case "pods":
			result, e = s.Manager.Kubectl(ctx, "-n", a.Namespace, "delete", "pod", a.ID, "--wait=false")
		default:
			e = errors.New("unknown resource kind")
		}
	case "pull":
		if a.Value == "" {
			e = errors.New("enter an image reference")
			break
		}
		result, e = s.Manager.Command(ctx, "docker", "--host", "unix://"+s.Manager.Paths.GatewaySocket(), "pull", a.Value)
	case "tag":
		result, e = s.Manager.Command(ctx, "docker", "--host", "unix://"+s.Manager.Paths.GatewaySocket(), "tag", a.ID, a.Value)
	case "createVolume":
		body, _ := json.Marshal(map[string]string{"Name": a.Value})
		result, e = s.Manager.Docker(ctx, "POST", "/volumes/create", strings.NewReader(string(body)))
	default:
		e = errors.New("unsupported action")
	}
	if e != nil {
		failure(w, e)
		return
	}
	respond(w, map[string]string{"text": string(result)})
}
func (s *Server) listForwards(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []Forward{}
	for _, f := range s.forwards {
		out = append(out, *f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	respond(w, out)
}
func (s *Server) createForward(w http.ResponseWriter, r *http.Request) {
	var f Forward
	if e := decode(r, &f); e != nil {
		failure(w, e)
		return
	}
	if f.LocalPort < 1024 || f.LocalPort > 65535 || f.RemotePort < 1 || f.RemotePort > 65535 || f.Pod == "" || strings.HasPrefix(f.Pod, "-") || f.Namespace == "" {
		failure(w, errors.New("select a pod, namespace, local port 1024–65535, and remote port 1–65535"))
		return
	}
	done, ok := s.Tracker.Admit(idle.Blocker{Kind: "portforward", Name: f.Namespace + "/" + f.Pod, Detail: fmt.Sprintf("localhost:%d → %d", f.LocalPort, f.RemotePort)})
	if !ok {
		failure(w, errors.New("start the engine first"))
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	f.cancel = cancel
	f.ID = strconv.FormatInt(time.Now().UnixNano(), 10)
	f.State = "connecting"
	f.cmd = exec.CommandContext(ctx, s.Manager.Paths.Binary("kubectl"), "--kubeconfig", s.Manager.Paths.Kubeconfig(), "-n", f.Namespace, "port-forward", "--address=127.0.0.1", "pod/"+f.Pod, fmt.Sprintf("%d:%d", f.LocalPort, f.RemotePort))
	pipe, e := f.cmd.StdoutPipe()
	if e != nil {
		cancel()
		done()
		failure(w, e)
		return
	}
	f.cmd.Stderr = f.cmd.Stdout
	if e = f.cmd.Start(); e != nil {
		cancel()
		done()
		failure(w, e)
		return
	}
	s.mu.Lock()
	s.forwards[f.ID] = &f
	s.mu.Unlock()
	go func() {
		defer done()
		defer cancel()
		buf := make([]byte, 4096)
		var output strings.Builder
		for {
			n, e := pipe.Read(buf)
			if n > 0 {
				chunk := string(buf[:n])
				if output.Len() < 8192 {
					output.WriteString(chunk)
				}
				if strings.Contains(chunk, "Forwarding from") {
					s.mu.Lock()
					f.State = "listening"
					s.mu.Unlock()
				}
			}
			if e != nil {
				break
			}
		}
		e := f.cmd.Wait()
		s.mu.Lock()
		f.State = "stopped"
		if e != nil && ctx.Err() == nil {
			f.State = "error"
			f.Error = output.String()
		}
		s.mu.Unlock()
	}()
	respond(w, map[string]string{"id": f.ID})
}
func (s *Server) deleteForward(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.forwards[r.PathValue("id")]
	if !ok {
		failure(w, errors.New("port forward not found"))
		return
	}
	f.cancel()
	delete(s.forwards, f.ID)
	respond(w, map[string]bool{"ok": true})
}

func Serve(ctx context.Context, s *Server) error {
	for _, path := range []string{s.Manager.Paths.Socket(), s.Manager.Paths.GatewaySocket()} {
		if _, e := os.Lstat(path); e == nil {
			conn, e := net.DialTimeout("unix", path, time.Second)
			if e == nil {
				conn.Close()
				return fmt.Errorf("another service is already listening on %s", path)
			}
			if e = os.Remove(path); e != nil {
				return e
			}
		}
	}
	control, e := gateway.ListenUnix(s.Manager.Paths.Socket())
	if e != nil {
		return e
	}
	defer control.Close()
	if e = os.Chmod(s.Manager.Paths.Socket(), 0600); e != nil {
		return e
	}
	docker, e := gateway.ListenUnix(s.Manager.Paths.GatewaySocket())
	if e != nil {
		return e
	}
	defer docker.Close()
	if e = os.Chmod(s.Manager.Paths.GatewaySocket(), 0600); e != nil {
		return e
	}
	target, _ := url.Parse("http://docker")
	dockerServer := &http.Server{Handler: gateway.Proxy(target, gateway.UnixTransport(s.Manager.Paths.DockerSocket()), s.Tracker, "Docker"), ReadHeaderTimeout: 10 * time.Second}
	controlServer := &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = dockerServer.Serve(docker) }()
	go s.RunMonitor(ctx)
	go func() { <-ctx.Done(); _ = controlServer.Close(); _ = dockerServer.Close() }()
	e = controlServer.Serve(control)
	if errors.Is(e, http.ErrServerClosed) {
		return nil
	}
	return e
}
