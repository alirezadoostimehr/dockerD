package gateway

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type bufferedTestConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c bufferedTestConn) Read(p []byte) (int, error) { return c.reader.Read(p) }

func TestBuildKitTracksRPCsRatherThanIdleConnections(t *testing.T) {
	for _, upgrade := range []bool{false, true} {
		t.Run(fmt.Sprint("upgrade=", upgrade), func(t *testing.T) {
			dir, err := os.MkdirTemp("/tmp", "dockerd-h2-")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(dir)
			upstreamPath := filepath.Join(dir, "engine.sock")
			gatewayPath := filepath.Join(dir, "docker.sock")
			finish := make(chan struct{})
			defer close(finish)
			upstream := &http.Server{Protocols: DockerProtocols(), Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(200)
				http.NewResponseController(w).Flush()
				if strings.HasSuffix(r.URL.Path, "ListenBuildHistory") {
					<-r.Context().Done()
					return
				}
				select {
				case <-finish:
				case <-r.Context().Done():
				}
			})}
			listener, e := net.Listen("unix", upstreamPath)
			if e != nil {
				t.Fatal(e)
			}
			go upstream.Serve(listener)
			defer upstream.Close()
			tracker := NewTracker()
			tracker.Gate(false)
			proxy := &http.Server{Protocols: DockerProtocols(), Handler: DockerProxy(upstreamPath, tracker)}
			listener, e = net.Listen("unix", gatewayPath)
			if e != nil {
				t.Fatal(e)
			}
			go proxy.Serve(listener)
			defer proxy.Close()
			transport := UnixTransport(gatewayPath)
			transport.Protocols = new(http.Protocols)
			transport.Protocols.SetUnencryptedHTTP2(true)
			if upgrade {
				transport.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
					conn, e := (&net.Dialer{}).DialContext(ctx, "unix", gatewayPath)
					if e != nil {
						return nil, e
					}
					fmt.Fprint(conn, "POST /grpc HTTP/1.1\r\nHost: docker\r\nConnection: Upgrade\r\nUpgrade: h2c\r\nContent-Length: 0\r\n\r\n")
					reader := bufio.NewReader(conn)
					response, e := http.ReadResponse(reader, nil)
					if e != nil {
						conn.Close()
						return nil, e
					}
					if response.StatusCode != 101 {
						conn.Close()
						return nil, fmt.Errorf("upgrade returned %d", response.StatusCode)
					}
					return bufferedTestConn{conn, reader}, nil
				}
			}
			client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
			defer transport.CloseIdleConnections()
			watch, e := client.Post("http://docker/moby.buildkit.v1.Control/ListenBuildHistory", "application/grpc", nil)
			if e != nil {
				t.Fatal(e)
			}
			defer watch.Body.Close()
			if active, _ := tracker.Snapshot(); len(active) != 0 {
				t.Fatalf("read-only history watch prevents idle: %+v", active)
			}
			solve, e := client.Post("http://docker/moby.buildkit.v1.Control/Solve", "application/grpc", nil)
			if e != nil {
				t.Fatal(e)
			}
			if active, _ := tracker.Snapshot(); len(active) != 1 {
				t.Fatalf("build RPC not tracked: %+v", active)
			}
			solve.Body.Close()
			deadline := time.Now().Add(time.Second)
			for time.Now().Before(deadline) {
				active, _ := tracker.Snapshot()
				if len(active) == 0 {
					break
				}
				time.Sleep(time.Millisecond)
			}
			active, generation := tracker.Snapshot()
			if len(active) != 0 || !tracker.TryGate(generation) {
				t.Fatal("completed build or idle connection still prevents shutdown")
			}
		})
	}
}
