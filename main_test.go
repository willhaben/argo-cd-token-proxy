package main

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestTokenCacheRefreshesBeforeExpiry(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	now := start
	exchanges := 0
	failNext := true
	cache := &tokenCache{
		now: func() time.Time { return now },
		exchange: func(context.Context) (dexToken, error) {
			exchanges++
			if failNext {
				failNext = false
				return dexToken{}, errors.New("dex down")
			}
			return dexToken{Value: fmt.Sprintf("token%d", exchanges), ExpiresIn: 10 * time.Minute}, nil
		},
	}
	get := func(at time.Time) string {
		t.Helper()
		now = at
		token, err := cache.get(t.Context())
		if err != nil {
			return ""
		}
		return token
	}

	if got := get(start); got != "" {
		t.Fatalf("failed exchange returned token %q", got)
	}
	if got := get(start); got != "token2" {
		t.Fatalf("after failure, got %q, want token2", got)
	}
	if got := get(start.Add(5 * time.Minute)); got != "token2" || exchanges != 2 {
		t.Fatalf("token inside lifetime was not cached: got %q after %d exchanges", got, exchanges)
	}
	if got := get(start.Add(9 * time.Minute)); got != "token3" || exchanges != 3 {
		t.Fatalf("token not refreshed before expiry: got %q after %d exchanges", got, exchanges)
	}
}
