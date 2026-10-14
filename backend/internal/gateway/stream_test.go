package gateway

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestUpgradeHeldUntilConnectionCloses(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, buf, e := w.(http.Hijacker).Hijack()
		if e != nil {
			return
		}
		defer conn.Close()
		fmt.Fprint(buf, "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: test\r\n\r\n")
		buf.Flush()
		bytes := make([]byte, 1)
		conn.Read(bytes)
	}))
	defer upstream.Close()
	target, _ := url.Parse(upstream.URL)
	tracker := NewTracker()
	tracker.Gate(false)
	proxy := httptest.NewServer(Proxy(target, http.DefaultTransport, tracker, "test"))
	defer proxy.Close()
	conn, e := net.Dial("tcp", strings.TrimPrefix(proxy.URL, "http://"))
	if e != nil {
		t.Fatal(e)
	}
	fmt.Fprint(conn, "GET /exec HTTP/1.1\r\nHost: localhost\r\nConnection: Upgrade\r\nUpgrade: test\r\n\r\n")
	reader := bufio.NewReader(conn)
	line, e := reader.ReadString('\n')
	if e != nil || !strings.Contains(line, "101") {
		t.Fatalf("upgrade: %s %v", line, e)
	}
	active, _ := tracker.Snapshot()
	if len(active) != 1 {
		t.Fatal("stream not tracked")
	}
	conn.Close()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		active, _ = tracker.Snapshot()
		if len(active) == 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("closed stream remained active")
}

func TestGRPCDisconnectDoesNotWaitForUpstreamHalfClose(t *testing.T) {
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, buf, e := w.(http.Hijacker).Hijack()
		if e != nil {
			return
		}
		defer conn.Close()
		fmt.Fprint(buf, "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: h2c\r\n\r\n")
		buf.Flush()
		// Simulate a VM tunnel that stays open after its client's write side closes.
		<-release
	}))
	defer upstream.Close()
	defer close(release)
	target, _ := url.Parse(upstream.URL)
	tracker := NewTracker()
	tracker.Gate(false)
	proxy := httptest.NewServer(Proxy(target, http.DefaultTransport, tracker, "Docker"))
	defer proxy.Close()
	conn, e := net.Dial("tcp", strings.TrimPrefix(proxy.URL, "http://"))
	if e != nil {
		t.Fatal(e)
	}
	fmt.Fprint(conn, "POST /grpc HTTP/1.1\r\nHost: localhost\r\nConnection: Upgrade\r\nUpgrade: h2c\r\nContent-Length: 0\r\n\r\n")
	line, e := bufio.NewReader(conn).ReadString('\n')
	if e != nil || !strings.Contains(line, "101") {
		t.Fatalf("upgrade: %s %v", line, e)
	}
	conn.Close()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		active, _ := tracker.Snapshot()
		if len(active) == 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("closed gRPC session retained an idle blocker")
}
