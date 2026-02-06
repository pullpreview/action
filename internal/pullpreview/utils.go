package pullpreview

import (
	"context"
	"time"
)

func WaitUntil(maxRetries int, interval time.Duration, fn func() bool) bool {
	return WaitUntilContext(context.Background(), maxRetries, interval, fn)
}

func WaitUntilContext(ctx context.Context, maxRetries int, interval time.Duration, fn func() bool) bool {
	ctx = EnsureContext(ctx)
	retries := 0
	for {
		select {
		case <-ctx.Done():
			return false
		default:
		}
		if fn() {
			return true
		}
		retries++
		if retries >= maxRetries {
			return false
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return false
		case <-timer.C:
		}
	}
}

func EnsureContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func ensureContext(ctx context.Context) context.Context {
	return EnsureContext(ctx)
}
