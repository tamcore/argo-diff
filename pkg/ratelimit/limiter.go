package ratelimit

import (
	"sync"
	"time"
)

// Limiter provides rate limiting per key (e.g., repository)
type Limiter struct {
	mu       sync.RWMutex
	limits   map[string]*bucket
	rate     int           // requests per window
	window   time.Duration // time window
	cleanupT *time.Ticker
	done     chan struct{}
}

type bucket struct {
	count   int
	resetAt time.Time
}

// NewLimiter creates a new rate limiter
// rate: number of requests allowed per window
// window: time window duration
func NewLimiter(rate int, window time.Duration) *Limiter {
	l := &Limiter{
		limits:   make(map[string]*bucket),
		rate:     rate,
		window:   window,
		cleanupT: time.NewTicker(window * 2),
		done:     make(chan struct{}),
	}
	go l.cleanup()
	return l
}

// Allow checks if a request for the given key is allowed
func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	b, exists := l.limits[key]

	if !exists || now.After(b.resetAt) {
		// New bucket or window expired
		l.limits[key] = &bucket{
			count:   1,
			resetAt: now.Add(l.window),
		}
		return true
	}

	if b.count >= l.rate {
		return false
	}

	b.count++
	return true
}

// Remaining returns the number of requests remaining for a key
func (l *Limiter) Remaining(key string) int {
	l.mu.RLock()
	defer l.mu.RUnlock()

	b, exists := l.limits[key]
	if !exists || time.Now().After(b.resetAt) {
		return l.rate
	}

	remaining := l.rate - b.count
	if remaining < 0 {
		return 0
	}
	return remaining
}

// Stop stops the rate limiter cleanup goroutine
func (l *Limiter) Stop() {
	close(l.done)
	l.cleanupT.Stop()
}

func (l *Limiter) cleanup() {
	for {
		select {
		case <-l.done:
			return
		case <-l.cleanupT.C:
			l.mu.Lock()
			now := time.Now()
			for key, b := range l.limits {
				if now.After(b.resetAt) {
					delete(l.limits, key)
				}
			}
			l.mu.Unlock()
		}
	}
}
