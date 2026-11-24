package ciretest

import (
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"
)

type readBackoff struct {
	mu    sync.Mutex
	until time.Time
	err   error
}

func (b *readBackoff) problem() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if time.Now().Before(b.until) {
		return b.err
	}
	return nil
}

func (b *readBackoff) record(response *http.Response, err error) {
	now := time.Now()
	var until time.Time
	if seconds, parseErr := strconv.ParseUint(response.Header.Get("Retry-After"), 10, 32); parseErr == nil {
		until = now.Add(time.Duration(seconds) * time.Second)
	} else if date, parseErr := http.ParseTime(response.Header.Get("Retry-After")); parseErr == nil {
		until = date
	}
	if response.Header.Get("X-RateLimit-Remaining") == "0" {
		if reset, parseErr := strconv.ParseInt(response.Header.Get("X-RateLimit-Reset"), 10, 64); parseErr == nil && time.Unix(reset, 0).After(until) {
			until = time.Unix(reset, 0)
		}
	}
	if !until.After(now) {
		until = now.Add(time.Minute)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if until.After(b.until) {
		b.until = until
		b.err = fmt.Errorf("GitHub reads deferred until %s after rate limit: %w", until.UTC().Format(time.RFC3339), err)
	}
}

func rateLimited(response *http.Response) bool {
	return response.StatusCode == http.StatusTooManyRequests ||
		(response.StatusCode == http.StatusForbidden &&
			(response.Header.Get("X-RateLimit-Remaining") == "0" || response.Header.Get("Retry-After") != ""))
}
