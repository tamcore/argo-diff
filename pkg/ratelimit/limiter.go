package ratelimit
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


























































































}	}		}			l.mu.Unlock()			}				}					delete(l.limits, key)				if now.After(b.resetAt) {			for key, b := range l.limits {			now := time.Now()			l.mu.Lock()		case <-l.cleanupT.C:			return		case <-l.done:		select {	for {func (l *Limiter) cleanup() {}	l.cleanupT.Stop()	close(l.done)func (l *Limiter) Stop() {// Stop stops the rate limiter cleanup goroutine}	return remaining	}		return 0	if remaining < 0 {	remaining := l.rate - b.count	}		return l.rate	if !exists || time.Now().After(b.resetAt) {	b, exists := l.limits[key]	defer l.mu.RUnlock()	l.mu.RLock()func (l *Limiter) Remaining(key string) int {// Remaining returns the number of requests remaining for a key}	return true	b.count++	}		return false	if b.count >= l.rate {	}		return true		}			resetAt: now.Add(l.window),			count:   1,		l.limits[key] = &bucket{		// New bucket or window expired	if !exists || now.After(b.resetAt) {	b, exists := l.limits[key]	now := time.Now()	defer l.mu.Unlock()	l.mu.Lock()func (l *Limiter) Allow(key string) bool {// Allow checks if a request for the given key is allowed}	return l	go l.cleanup()	}		done:     make(chan struct{}),		cleanupT: time.NewTicker(window * 2),		window:   window,		rate:     rate,		limits:   make(map[string]*bucket),	l := &Limiter{func NewLimiter(rate int, window time.Duration) *Limiter {// window: time window duration// rate: number of requests allowed per window// NewLimiter creates a new rate limiter}	resetAt  time.Time	count    inttype bucket struct {}	done     chan struct{}	cleanupT *time.Ticker	window   time.Duration // time window