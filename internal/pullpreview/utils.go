package pullpreview

import "time"

func WaitUntil(maxRetries int, interval time.Duration, fn func() bool) bool {
	retries := 0
	for {
		if fn() {
			return true
		}
		retries++
		if retries >= maxRetries {
			return false
		}
		time.Sleep(interval)
	}
}
