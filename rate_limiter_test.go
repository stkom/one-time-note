package main

import (
	"testing"
	"time"
)

func TestRateLimiterExhaustsBurstAndReportsRetryAfter(t *testing.T) {
	limiter := newTestRateLimiter(
		new(time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)),
		RateSpec{Requests: 2, Window: time.Minute, Burst: 2})

	assertAllowed(t, limiter, "client-a")
	assertAllowed(t, limiter, "client-a")
	allowed, retryAfter := limiter.AllowRequest("client-a")

	if allowed {
		t.Fatal("third request was allowed, want rate limited")
	}
	if retryAfter != 30*time.Second {
		t.Fatalf("retryAfter = %s, want 30s", retryAfter)
	}
}

func TestRateLimiterRefillsOverTime(t *testing.T) {
	now := time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
	limiter := newTestRateLimiter(&now, RateSpec{Requests: 2, Window: time.Minute, Burst: 1})

	assertAllowed(t, limiter, "client-a")

	now = now.Add(29 * time.Second)
	allowed, retryAfter := limiter.AllowRequest("client-a")
	if allowed {
		t.Fatal("request after 29s was allowed, want rate limited")
	}
	if retryAfter != time.Second {
		t.Fatalf("retryAfter = %s, want 1s", retryAfter)
	}

	now = now.Add(time.Second)
	assertAllowed(t, limiter, "client-a")
}

func TestRateLimiterUsesIndependentClientBuckets(t *testing.T) {
	limiter := newTestRateLimiter(
		new(time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)),
		RateSpec{Requests: 1, Window: time.Minute, Burst: 1})

	assertAllowed(t, limiter, "client-a")
	assertRateLimited(t, limiter, "client-a")
	assertAllowed(t, limiter, "client-b")
}

func TestRateLimiterEvictsIdleBuckets(t *testing.T) {
	now := time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
	limiter := newTestRateLimiter(&now, RateSpec{Requests: 1, Window: time.Minute, Burst: 1})

	assertAllowed(t, limiter, "client-a")
	if len(limiter.buckets) != 1 {
		t.Fatalf("bucket count = %d, want 1", len(limiter.buckets))
	}

	now = now.Add(rateLimiterMinBucketTTL)
	assertAllowed(t, limiter, "client-b")

	if _, ok := limiter.buckets["client-a"]; ok {
		t.Fatal("stale client-a bucket was not evicted")
	}
	if _, ok := limiter.buckets["client-b"]; !ok {
		t.Fatal("active client-b bucket was not retained")
	}
}

func TestRateLimiterEvictsOldestIdleBucketWhenFull(t *testing.T) {
	now := time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
	limiter := newTestRateLimiter(&now, RateSpec{Requests: 100, Window: time.Minute, Burst: 100})
	limiter.maxBuckets = 2

	assertAllowed(t, limiter, "client-a")
	now = now.Add(30 * time.Second)
	assertAllowed(t, limiter, "client-b")
	now = now.Add(30 * time.Second)
	assertAllowed(t, limiter, "client-c")

	if len(limiter.buckets) != 2 {
		t.Fatalf("bucket count = %d, want 2", len(limiter.buckets))
	}
	if _, ok := limiter.buckets["client-a"]; ok {
		t.Fatal("oldest idle client-a bucket was not evicted")
	}
	if _, ok := limiter.buckets["client-b"]; !ok {
		t.Fatal("recent client-b bucket was evicted before older idle bucket")
	}
	if _, ok := limiter.buckets["client-c"]; !ok {
		t.Fatal("new client-c bucket was not created")
	}
}

func TestRateLimiterRejectsNewBucketWhenFullOfActiveBuckets(t *testing.T) {
	now := time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
	limiter := newTestRateLimiter(&now, RateSpec{Requests: 100, Window: time.Minute, Burst: 100})
	limiter.maxBuckets = 1

	assertAllowed(t, limiter, "client-a")
	now = now.Add(rateLimiterSaturationIdleAge - time.Nanosecond)
	allowed, retryAfter := limiter.AllowRequest("client-b")

	if allowed {
		t.Fatal("new client bucket was allowed while limiter was saturated with active buckets")
	}
	if retryAfter != time.Second {
		t.Fatalf("retryAfter = %s, want 1s", retryAfter)
	}
	if len(limiter.buckets) != 1 {
		t.Fatalf("bucket count = %d, want 1", len(limiter.buckets))
	}
	if _, ok := limiter.buckets["client-a"]; !ok {
		t.Fatal("active client-a bucket was evicted")
	}
}

func newTestRateLimiter(now *time.Time, spec RateSpec) *RateLimiter {
	limiter := NewRateLimiter(spec)
	limiter.now = func() time.Time {
		return *now
	}
	return limiter
}

func assertAllowed(t *testing.T, limiter *RateLimiter, clientKey string) {
	t.Helper()
	allowed, retryAfter := limiter.AllowRequest(clientKey)
	if !allowed || retryAfter != 0 {
		t.Fatalf("allowed = %v retryAfter = %s, want allowed", allowed, retryAfter)
	}
}

func assertRateLimited(t *testing.T, limiter *RateLimiter, clientKey string) {
	t.Helper()
	allowed, retryAfter := limiter.AllowRequest(clientKey)
	if allowed {
		t.Fatal("request was allowed, want rate limited")
	}
	if retryAfter <= 0 {
		t.Fatalf("retryAfter = %s, want positive duration", retryAfter)
	}
}
