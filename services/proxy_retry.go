package services

import (
	"context"
	"io"
	"math/rand/v2"
	"net/http"
	"time"

	"github.com/wfu-work/free-ai-go/domains"
)

const upstreamCapacityRetryTimeout = 5 * time.Minute

const errUpstreamCapacityRetryTimeout streamTimeoutError = "upstream did not become available within the 5 minute retry window"

// The deadline covers retries and waiting for the first usable response. Once
// content is sent, normal stream idle timeouts take over; replay is unsafe.
func newProxyRetryContext(parent context.Context) (context.Context, *streamTimeoutController) {
	ctx, cancel := context.WithCancelCause(parent)
	controller := &streamTimeoutController{cancel: cancel}
	controller.arm(upstreamCapacityRetryTimeout, errUpstreamCapacityRetryTimeout)
	return ctx, controller
}

type proxyRetryWriter struct {
	io.Writer
	timeout *streamTimeoutController
}

func (w proxyRetryWriter) Write(p []byte) (int, error) {
	if len(p) > 0 {
		w.timeout.stop()
	}
	return w.Writer.Write(p)
}

func (w proxyRetryWriter) Flush() {
	if flusher, ok := w.Writer.(http.Flusher); ok {
		flusher.Flush()
	}
}

func shouldWaitForUpstreamCapacity(result *ProxyResult, err error, stream bool) bool {
	if !shouldRetry(result, err, stream) {
		return false
	}
	return IsTransientUpstreamCapacityError(result, err) ||
		(result != nil && (result.StatusCode == http.StatusTooManyRequests || result.ErrorType == domains.ErrorRateLimited)) ||
		classifyError(err) == domains.ErrorRateLimited
}

func upstreamRetryDelay(failures int, header http.Header) time.Duration {
	// Bounded exponential backoff with jitter prevents simultaneous callers
	// from repeatedly hammering the same upstream during a capacity incident.
	delay := time.Second
	for i := 1; i < failures && delay < 15*time.Second; i++ {
		delay *= 2
	}
	if delay > 15*time.Second {
		delay = 15 * time.Second
	}
	delay += time.Duration(rand.Int64N(int64(delay / 4)))
	if until := retryAfterUntil(header, time.Now()); until > 0 {
		if retryAfter := time.Until(time.UnixMilli(until)); retryAfter > delay {
			delay = retryAfter
		}
	}
	return delay
}

func waitForUpstreamRetry(ctx context.Context, delay time.Duration) {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}
