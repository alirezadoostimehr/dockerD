package gateway

import (
	"bufio"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Docker's BuildKit API multiplexes operations and read-only watches over a
// persistent HTTP/2 connection. Track each RPC, not the lifetime of that socket.
// Both native h2c and the older POST /grpc upgrade are supported.
func DockerProxy(socket string, tracker *Tracker) http.Handler {
	h1 := UnixTransport(socket)
	h2 := UnixTransport(socket)
	h2.Protocols = new(http.Protocols)
	h2.Protocols.SetUnencryptedHTTP2(true)
	target, _ := url.Parse("http://docker")
	proxy := Proxy(target, protocolTransport{h1, h2}, tracker, "Docker")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/grpc") && strings.EqualFold(r.Header.Get("Upgrade"), "h2c") {
			tracker.mu.Lock()
			gated := tracker.gated
			tracker.mu.Unlock()
			if gated {
				http.Error(w, "DockerD engine is stopped. Start it from the app.", 503)
				return
			}
			serveH2Upgrade(w, r, proxy)
			return
		}
		proxy.ServeHTTP(w, r)
	})
}

type protocolTransport struct{ h1, h2 http.RoundTripper }

func (t protocolTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.ProtoMajor == 2 {
		return t.h2.RoundTrip(r)
	}
	return t.h1.RoundTrip(r)
}

func DockerProtocols() *http.Protocols {
	p := new(http.Protocols)
	p.SetHTTP1(true)
	p.SetUnencryptedHTTP2(true)
	return p
}

type bufferedConnection struct {
	net.Conn
	reader *bufio.Reader
	closed chan struct{}
	once   sync.Once
}

func (c *bufferedConnection) Read(p []byte) (int, error) { return c.reader.Read(p) }
func (c *bufferedConnection) Close() error {
	c.once.Do(func() { close(c.closed) })
	return c.Conn.Close()
}

type singleConnectionListener struct {
	conn     *bufferedConnection
	accepted bool
}

func (l *singleConnectionListener) Accept() (net.Conn, error) {
	if !l.accepted {
		l.accepted = true
		return l.conn, nil
	}
	<-l.conn.closed
	return nil, net.ErrClosed
}
func (l *singleConnectionListener) Close() error   { return l.conn.Close() }
func (l *singleConnectionListener) Addr() net.Addr { return l.conn.LocalAddr() }

func serveH2Upgrade(w http.ResponseWriter, r *http.Request, handler http.Handler) {
	connection, buffer, e := http.NewResponseController(w).Hijack()
	if e != nil {
		http.Error(w, "Cannot upgrade Docker connection", 500)
		return
	}
	defer connection.Close()
	if _, e = buffer.WriteString("HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: h2c\r\n\r\n"); e != nil {
		return
	}
	if e = buffer.Flush(); e != nil {
		return
	}
	conn := &bufferedConnection{Conn: connection, reader: buffer.Reader, closed: make(chan struct{})}
	listener := &singleConnectionListener{conn: conn}
	defer listener.Close()
	protocols := new(http.Protocols)
	protocols.SetUnencryptedHTTP2(true)
	server := &http.Server{Handler: handler, Protocols: protocols, ReadHeaderTimeout: 10 * time.Second}
	if e = server.Serve(listener); e != nil && !errors.Is(e, net.ErrClosed) && !errors.Is(e, http.ErrServerClosed) {
		_ = server.Close()
	}
}
