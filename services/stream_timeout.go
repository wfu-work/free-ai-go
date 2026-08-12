package services

import (
	"context"
	"sync"
	"time"
)

type streamTimeoutError string

func (e streamTimeoutError) Error() string { return string(e) }

func (e streamTimeoutError) Unwrap() error { return context.DeadlineExceeded }

const (
	errStreamFirstResponseTimeout streamTimeoutError = "upstream first response timeout"
	errStreamIdleTimeout          streamTimeoutError = "upstream stream idle timeout"
)

// streamTimeoutController 在首个 SSE 前执行首响应超时；首个事件到达后，
// 将其替换为每次收到事件都会重置的流式空闲超时。
type streamTimeoutController struct {
	mu         sync.Mutex
	cancel     context.CancelCauseFunc
	timer      *time.Timer
	generation uint64
	done       bool
}

func newStreamTimeoutContext(parent context.Context, firstResponseTimeout time.Duration) (context.Context, *streamTimeoutController) {
	ctx, cancel := context.WithCancelCause(parent)
	controller := &streamTimeoutController{cancel: cancel}
	controller.arm(firstResponseTimeout, errStreamFirstResponseTimeout)
	return ctx, controller
}

func (c *streamTimeoutController) noteEvent(idleTimeout time.Duration) {
	c.arm(idleTimeout, errStreamIdleTimeout)
}

func (c *streamTimeoutController) arm(timeout time.Duration, cause error) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.done {
		return
	}
	c.generation++
	generation := c.generation
	if c.timer != nil {
		c.timer.Stop()
		c.timer = nil
	}
	if timeout <= 0 {
		return
	}
	c.timer = time.AfterFunc(timeout, func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.done || c.generation != generation {
			return
		}
		c.done = true
		c.timer = nil
		c.cancel(cause)
	})
}

func (c *streamTimeoutController) stop() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.done {
		return
	}
	c.done = true
	c.generation++
	if c.timer != nil {
		c.timer.Stop()
		c.timer = nil
	}
}

// streamContextError 优先保留客户端取消；否则把内部计时器的具体原因
// 暴露给统一错误分类，使首响应与流式空闲超时都归类为 upstream_timeout。
func streamContextError(streamCtx, parent context.Context, fallback error) error {
	if parent != nil && parent.Err() != nil {
		return parent.Err()
	}
	if streamCtx != nil {
		if cause := context.Cause(streamCtx); cause != nil {
			return cause
		}
	}
	return fallback
}
