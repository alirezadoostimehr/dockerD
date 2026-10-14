// Package gateway tracks requests for their full lifetime, including HTTP upgrades.
package gateway

import (
	"context"
	"dockerd.local/backend/internal/idle"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

type Tracker struct {
	mu         sync.Mutex
	gated      bool
	next       uint64
	active     map[uint64]idle.Blocker
	generation uint64
}

func NewTracker() *Tracker { return &Tracker{gated: true, active: map[uint64]idle.Blocker{}} }
func (t *Tracker) Admit(b idle.Blocker) (func(), bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.gated {
		return nil, false
	}
	t.next++
	id := t.next
	t.active[id] = b
	t.generation++
	return func() { t.mu.Lock(); delete(t.active, id); t.generation++; t.mu.Unlock() }, true
}
func (t *Tracker) Gate(on bool) { t.mu.Lock(); t.gated = on; t.mu.Unlock() }
func (t *Tracker) Snapshot() ([]idle.Blocker, uint64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	b := []idle.Blocker{}
	for _, v := range t.active {
		b = append(b, v)
	}
	sort.Slice(b, func(i, j int) bool { return b[i].Name < b[j].Name })
	return b, t.generation
}
func (t *Tracker) TryGate(generation uint64) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.generation != generation || len(t.active) > 0 || t.gated {
		return false
	}
	t.gated = true
	return true
}

func IsActivity(r *http.Request) bool {
	p := r.URL.Path
	switch p {
	case "/moby.buildkit.v1.Control/Info", "/moby.buildkit.v1.Control/ListWorkers", "/moby.buildkit.v1.Control/DiskUsage", "/moby.buildkit.v1.Control/ListenBuildHistory", "/moby.buildkit.v1.Control/Status", "/grpc.health.v1.Health/Check", "/grpc.health.v1.Health/Watch":
		return false
	}
	transfer := (strings.Contains(p, "/images/") && strings.HasSuffix(p, "/get")) || strings.HasSuffix(p, "/archive") || strings.HasSuffix(p, "/export")
	return r.Method != "GET" && r.Method != "HEAD" || transfer || strings.EqualFold(r.Header.Get("Connection"), "upgrade") || r.Header.Get("Upgrade") != "" || strings.Contains(p, "/exec") || strings.Contains(p, "/attach") || strings.Contains(p, "/portforward")
}

func Proxy(target *url.URL, transport http.RoundTripper, t *Tracker, name string) http.Handler {
	p := httputil.NewSingleHostReverseProxy(target)
	p.Transport = upgradeTransport{transport}
	p.FlushInterval = -1
	p.ErrorHandler = func(w http.ResponseWriter, r *http.Request, e error) {
		http.Error(w, "DockerD engine unavailable. Start it from DockerD. "+e.Error(), http.StatusServiceUnavailable)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.mu.Lock()
		gated := t.gated
		t.mu.Unlock()
		if gated {
			http.Error(w, "DockerD engine is stopped or stopping. Start it from the app.", 503)
			return
		}
		if IsActivity(r) {
			done, ok := t.Admit(idle.Blocker{Kind: "operation", Name: name + " " + r.Method + " " + r.URL.Path, Detail: "An API operation or streaming session is open."})
			if !ok {
				http.Error(w, "Engine is stopping", 503)
				return
			}
			defer done()
		}
		p.ServeHTTP(w, r)
	})
}

type upgradeTransport struct{ http.RoundTripper }
type fullCloseStream struct{ io.ReadWriteCloser }

func (t upgradeTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	response, e := t.RoundTripper.RoundTrip(r)
	if e == nil && response.StatusCode == http.StatusSwitchingProtocols && strings.EqualFold(r.Header.Get("Upgrade"), "h2c") {
		if stream, ok := response.Body.(io.ReadWriteCloser); ok {
			// HTTP/2 gRPC sessions do not use TCP half-close for stdin. Hide CloseWrite
			// so ReverseProxy closes both ends on client EOF instead of retaining a
			// dead session through a VM tunnel that cannot propagate half-closes.
			// Raw Docker exec streams keep their ordinary half-close behavior.
			response.Body = fullCloseStream{stream}
		}
	}
	return response, e
}
func UnixTransport(path string) *http.Transport {
	return &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "unix", path)
	}, ResponseHeaderTimeout: 30 * time.Second}
}
func ListenUnix(path string) (net.Listener, error) {
	l, e := net.Listen("unix", path)
	if e != nil {
		return nil, fmt.Errorf("listen %s: %w", path, e)
	}
	return l, nil
}
