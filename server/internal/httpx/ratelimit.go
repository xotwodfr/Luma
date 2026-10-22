package httpx

import (
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// Limiter is a set of token buckets keyed by string (an IP or a user id).
// ponytail: in-memory and per-process; move to Postgres or Redis if Luma ever runs more than one instance.
type Limiter struct {
	capacity float64
	per      time.Duration
	mu       sync.Mutex
	buckets  map[string]*bucket
	lastGC   time.Time
	now      func() time.Time
}

type bucket struct {
	tokens float64
	at     time.Time
}

// NewLimiter allows n events per window per key, refilling continuously.
func NewLimiter(n int, window time.Duration) *Limiter {
	return &Limiter{capacity: float64(n), per: window, buckets: map[string]*bucket{}, now: time.Now}
}

// Allow takes one token for key. When empty it reports how long until one is available.
func (l *Limiter) Allow(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	rate := l.capacity / float64(l.per)
	if now.Sub(l.lastGC) > l.per {
		for k, b := range l.buckets {
			if now.Sub(b.at) > l.per {
				delete(l.buckets, k)
			}
		}
		l.lastGC = now
	}
	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{tokens: l.capacity, at: now}
		l.buckets[key] = b
	}
	b.tokens = math.Min(l.capacity, b.tokens+float64(now.Sub(b.at))*rate)
	b.at = now
	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	return false, time.Duration((1 - b.tokens) / rate)
}

// Limit writes 429 with Retry-After and returns false when key is over its limit.
func (l *Limiter) Limit(w http.ResponseWriter, key string) bool {
	ok, wait := l.Allow(key)
	if !ok {
		w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(wait.Seconds()))))
		WriteError(w, http.StatusTooManyRequests, "rate_limited", "Too many requests. Try again shortly.")
	}
	return ok
}
