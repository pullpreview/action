package pullpreview

import (
	"testing"
	"time"
)

func TestPrExpired(t *testing.T) {
	now := time.Now()
	if prExpired(now.Add(-2*time.Hour), "3h") {
		t.Fatalf("expected not expired")
	}
	if !prExpired(now.Add(-5*time.Hour), "3h") {
		t.Fatalf("expected expired")
	}
	if !prExpired(now.Add(-48*time.Hour), "2d") {
		t.Fatalf("expected expired")
	}
	if prExpired(now, "infinite") {
		t.Fatalf("expected not expired")
	}
}
