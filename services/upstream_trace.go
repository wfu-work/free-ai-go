package services

import (
	"context"
	"crypto/tls"
	"net/http/httptrace"
	"sync"
	"time"
)

// upstreamRequestTrace 采集单次上游 HTTP 请求的连接与响应头阶段耗时。
type upstreamRequestTrace struct {
	mu sync.Mutex

	startedAt        time.Time
	dnsStartedAt     time.Time
	connectStarted   time.Time
	tlsStartedAt     time.Time
	dnsMs            int64
	connectMs        int64
	tlsHandshakeMs   int64
	upstreamHeaderMs int64
	connectionReused bool
	gotConnection    bool
}

type upstreamTraceSnapshot struct {
	DNSMs            int64
	ConnectMs        int64
	TLSHandshakeMs   int64
	UpstreamHeaderMs int64
	ConnectionReused bool
	GotConnection    bool
}

func newUpstreamRequestTrace() *upstreamRequestTrace {
	return &upstreamRequestTrace{startedAt: time.Now()}
}

func (t *upstreamRequestTrace) context(parent context.Context) context.Context {
	if t == nil {
		return parent
	}
	trace := &httptrace.ClientTrace{
		DNSStart: func(httptrace.DNSStartInfo) {
			t.mu.Lock()
			t.dnsStartedAt = time.Now()
			t.mu.Unlock()
		},
		DNSDone: func(httptrace.DNSDoneInfo) {
			t.mu.Lock()
			if !t.dnsStartedAt.IsZero() {
				t.dnsMs = elapsedMilliseconds(t.dnsStartedAt)
			}
			t.mu.Unlock()
		},
		ConnectStart: func(_, _ string) {
			t.mu.Lock()
			t.connectStarted = time.Now()
			t.mu.Unlock()
		},
		ConnectDone: func(_, _ string, err error) {
			t.mu.Lock()
			if err == nil && !t.connectStarted.IsZero() {
				t.connectMs = elapsedMilliseconds(t.connectStarted)
			}
			t.mu.Unlock()
		},
		TLSHandshakeStart: func() {
			t.mu.Lock()
			t.tlsStartedAt = time.Now()
			t.mu.Unlock()
		},
		TLSHandshakeDone: func(tls.ConnectionState, error) {
			t.mu.Lock()
			if !t.tlsStartedAt.IsZero() {
				t.tlsHandshakeMs = elapsedMilliseconds(t.tlsStartedAt)
			}
			t.mu.Unlock()
		},
		GotConn: func(info httptrace.GotConnInfo) {
			t.mu.Lock()
			t.connectionReused = info.Reused
			t.gotConnection = true
			t.mu.Unlock()
		},
		GotFirstResponseByte: func() {
			t.mu.Lock()
			t.upstreamHeaderMs = elapsedMilliseconds(t.startedAt)
			t.mu.Unlock()
		},
	}
	return httptrace.WithClientTrace(parent, trace)
}

func (t *upstreamRequestTrace) snapshot() upstreamTraceSnapshot {
	if t == nil {
		return upstreamTraceSnapshot{}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return upstreamTraceSnapshot{
		DNSMs:            t.dnsMs,
		ConnectMs:        t.connectMs,
		TLSHandshakeMs:   t.tlsHandshakeMs,
		UpstreamHeaderMs: t.upstreamHeaderMs,
		ConnectionReused: t.connectionReused,
		GotConnection:    t.gotConnection,
	}
}

func elapsedMilliseconds(start time.Time) int64 {
	if start.IsZero() {
		return 0
	}
	return time.Since(start).Milliseconds()
}
