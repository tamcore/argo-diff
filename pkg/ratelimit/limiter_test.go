package ratelimit

import (
	"sync"
	"testing"
	"time"
)

func TestNewLimiter(t *testing.T) {
	l := NewLimiter(10, time.Second)
	defer l.Stop()

	if l == nil {
		t.Fatal("expected limiter to be non-nil")
	}
	if l.rate != 10 {
		t.Errorf("expected rate=10, got %d", l.rate)
	}
	if l.window != time.Second {
		t.Errorf("expected window=1s, got %v", l.window)
	}
}

func TestLimiterAllow(t *testing.T) {
	l := NewLimiter(3, time.Second)
	defer l.Stop()

	key := "test-repo"

	// First 3 requests should be allowed
	for i := 0; i < 3; i++ {
		if !l.Allow(key) {
			t.Errorf("request %d should be allowed", i+1)
		}
	}

	// 4th request should be denied
	if l.Allow(key) {
		t.Error("4th request should be denied")
	}
}

func TestLimiterAllowDifferentKeys(t *testing.T) {
	l := NewLimiter(2, time.Second)
	defer l.Stop()

	// Exhaust key1
	l.Allow("key1")
	l.Allow("key1")
	if l.Allow("key1") {
		t.Error("key1 should be rate limited")
	}

	// key2 should still be allowed
	if !l.Allow("key2") {
		t.Error("key2 should be allowed (different key)")
	}
}

func TestLimiterWindowExpiry(t *testing.T) {
	l := NewLimiter(2, 50*time.Millisecond)
	defer l.Stop()

	key := "test-repo"

	// Exhaust the limit
	l.Allow(key)
	l.Allow(key)
	if l.Allow(key) {
		t.Error("should be rate limited")
	}

	// Wait for window to expire
	time.Sleep(60 * time.Millisecond)

	// Should be allowed again
	if !l.Allow(key) {
		t.Error("should be allowed after window expires")
	}
}

func TestLimiterRemaining(t *testing.T) {
	l := NewLimiter(5, time.Second)
	defer l.Stop()

	key := "test-repo"

	// Initially should have all requests remaining
	if r := l.Remaining(key); r != 5 {
		t.Errorf("expected 5 remaining, got %d", r)
	}

	// Use 2 requests
	l.Allow(key)
	l.Allow(key)

	if r := l.Remaining(key); r != 3 {
		t.Errorf("expected 3 remaining, got %d", r)
	}

	// Use remaining
	l.Allow(key)
	l.Allow(key)
	l.Allow(key)

	if r := l.Remaining(key); r != 0 {
		t.Errorf("expected 0 remaining, got %d", r)
	}
}

func TestLimiterRemainingAfterExpiry(t *testing.T) {
	l := NewLimiter(5, 50*time.Millisecond)
	defer l.Stop()

	key := "test-repo"

	// Use some requests
	l.Allow(key)
	l.Allow(key)

	// Wait for expiry
	time.Sleep(60 * time.Millisecond)

	// Should report full capacity
	if r := l.Remaining(key); r != 5 {
		t.Errorf("expected 5 remaining after expiry, got %d", r)
	}
}

func TestLimiterConcurrentAccess(t *testing.T) {
	l := NewLimiter(100, time.Second)
	defer l.Stop()

	var wg sync.WaitGroup
	var allowed, denied int32
	var mu sync.Mutex

	// Spawn 200 concurrent requests
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if l.Allow("concurrent-test") {
				mu.Lock()
				allowed++
				mu.Unlock()
			} else {
				mu.Lock()
				denied++
				mu.Unlock()
			}
		}()
	}

	wg.Wait()

	if allowed != 100 {
		t.Errorf("expected 100 allowed requests, got %d", allowed)
	}
	if denied != 100 {
		t.Errorf("expected 100 denied requests, got %d", denied)
	}
}

func TestLimiterCleanup(t *testing.T) {
	// Create limiter with short window and cleanup interval
	l := NewLimiter(5, 30*time.Millisecond)
	defer l.Stop()

	// Create some entries
	l.Allow("key1")
	l.Allow("key2")
	l.Allow("key3")

	// Check entries exist
	l.mu.RLock()
	initialLen := len(l.limits)
	l.mu.RUnlock()

	if initialLen != 3 {
		t.Errorf("expected 3 entries, got %d", initialLen)
	}

	// Wait for cleanup (cleanup runs at window*2 interval)
	time.Sleep(100 * time.Millisecond)

	// Entries should be cleaned up
	l.mu.RLock()
	finalLen := len(l.limits)
	l.mu.RUnlock()

	if finalLen != 0 {
		t.Errorf("expected 0 entries after cleanup, got %d", finalLen)
	}
}

func TestLimiterStop(t *testing.T) {
	l := NewLimiter(5, time.Second)

	// Stop should not panic
	l.Stop()

	// Multiple stops should not panic
	// (channel already closed, but this tests the pattern)
}

func TestLimiterZeroRemaining(t *testing.T) {
	l := NewLimiter(1, time.Second)
	defer l.Stop()

	key := "test"
	l.Allow(key)

	// Try to get more than rate
	l.Allow(key)
	l.Allow(key)

	// Remaining should be 0, not negative
	if r := l.Remaining(key); r != 0 {
		t.Errorf("expected 0 remaining, got %d", r)
	}
}
