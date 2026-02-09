package main

import (
	"sync"
	"time"

	"golang.org/x/time/rate"
)

const (
	rateLimiterSweepInterval     = 5 * time.Minute
	rateLimiterMinBucketTTL      = time.Hour
	rateLimiterSaturationIdleAge = time.Minute
	rateLimiterMaxBuckets        = 4096
)

type RateLimiter struct {
	mu         sync.Mutex
	buckets    map[string]*rateBucket
	spec       RateSpec
	now        func() time.Time
	lastSweep  time.Time
	maxBuckets int
}

type rateBucket struct {
	limiter  *rate.Limiter
	lastSeen time.Time
	ttl      time.Duration
}

func NewRateLimiter(spec RateSpec) *RateLimiter {
	return &RateLimiter{
		buckets:    make(map[string]*rateBucket),
		spec:       spec,
		now:        time.Now,
		maxBuckets: rateLimiterMaxBuckets,
	}
}

func (l *RateLimiter) AllowRequest(clientKey string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	l.sweepExpiredBuckets(now)

	bucket := l.buckets[clientKey]
	if bucket == nil {
		if !l.ensureBucketCapacity(clientKey, now) {
			return false, time.Second
		}
		bucket = &rateBucket{
			limiter:  rate.NewLimiter(rateLimit(l.spec), l.spec.Burst),
			lastSeen: now,
			ttl:      rateBucketTTL(l.spec),
		}
		l.buckets[clientKey] = bucket
	}
	bucket.lastSeen = now

	if bucket.limiter.AllowN(now, 1) {
		return true, 0
	}

	tokens := bucket.limiter.TokensAt(now)
	if tokens < 0 {
		tokens = 0
	}
	needed := 1 - tokens
	retry := time.Duration(float64(time.Second) * needed / float64(rateLimit(l.spec)))
	if retry < time.Second {
		retry = time.Second
	}
	return false, retry
}

func (l *RateLimiter) ensureBucketCapacity(newBucketKey string, now time.Time) bool {
	if l.maxBuckets <= 0 || len(l.buckets) < l.maxBuckets {
		return true
	}

	var oldestKey string
	var oldestSeen time.Time
	for key, bucket := range l.buckets {
		if key == newBucketKey || now.Sub(bucket.lastSeen) < rateLimiterSaturationIdleAge {
			continue
		}
		if oldestKey == "" || bucket.lastSeen.Before(oldestSeen) {
			oldestKey = key
			oldestSeen = bucket.lastSeen
		}
	}
	if oldestKey != "" {
		delete(l.buckets, oldestKey)
		return true
	}

	logSecurityEvent("rate_limiter_saturated", "bucket_count", len(l.buckets))
	return false
}

func (l *RateLimiter) sweepExpiredBuckets(now time.Time) {
	if !l.lastSweep.IsZero() && now.Sub(l.lastSweep) < rateLimiterSweepInterval {
		return
	}
	l.lastSweep = now

	for key, bucket := range l.buckets {
		if now.Sub(bucket.lastSeen) >= bucket.ttl {
			delete(l.buckets, key)
		}
	}
}

func rateLimit(spec RateSpec) rate.Limit {
	return rate.Limit(float64(spec.Requests) / spec.Window.Seconds())
}

func rateBucketTTL(spec RateSpec) time.Duration {
	ttl := 2 * spec.Window
	if ttl < rateLimiterMinBucketTTL {
		return rateLimiterMinBucketTTL
	}
	return ttl
}
