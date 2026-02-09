package main

import (
	"context"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRunServerReturnsListenFailure(t *testing.T) {
	app := &app{Cfg: &Config{CleanupInterval: time.Hour}}
	server := &http.Server{
		Addr:    "127.0.0.1:not-a-port",
		Handler: http.NotFoundHandler(),
	}

	err := runServer(context.Background(), server, app)

	if err == nil || !strings.Contains(err.Error(), "error listening") {
		t.Fatalf("runServer error = %v, want listen failure", err)
	}
}

func TestRunServerReturnsNilOnContextShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	app := &app{Cfg: &Config{CleanupInterval: time.Hour}}
	server := &http.Server{
		Addr:    "127.0.0.1:8080",
		Handler: http.NotFoundHandler(),
	}

	if err := serveServer(ctx, server, app, newBlockingListener()); err != nil {
		t.Fatalf("serveServer returned error: %v", err)
	}
}

type blockingListener struct {
	closed chan struct{}
	once   sync.Once
}

func newBlockingListener() *blockingListener {
	return &blockingListener{closed: make(chan struct{})}
}

func (l *blockingListener) Accept() (net.Conn, error) {
	<-l.closed
	return nil, net.ErrClosed
}

func (l *blockingListener) Close() error {
	l.once.Do(func() {
		close(l.closed)
	})
	return nil
}

func (l *blockingListener) Addr() net.Addr {
	return testAddr("127.0.0.1:8080")
}

type testAddr string

func (a testAddr) Network() string {
	return "tcp"
}

func (a testAddr) String() string {
	return string(a)
}
