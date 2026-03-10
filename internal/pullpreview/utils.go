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

func pollAttemptsForWindow(window, interval time.Duration) int {
	if window <= 0 || interval <= 0 {
		return 1
	}

	// WaitUntilContext checks once before sleeping, so add one attempt to cover
	// the full window when the probe fails quickly.
	attempts := int(window / interval)
	if window%interval != 0 {
		attempts++
	}
	return attempts + 1
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
