package fx

import (
	"context"
	"errors"
	"testing"
	"time"
)

type stubProvider struct {
	calls  int
	quote  Quote
	err    error
	onCall func(calls int) (Quote, error)
}

func (s *stubProvider) GetQuote(_ context.Context, from, to string, amount float64) (Quote, error) {
	s.calls++
	if s.onCall != nil {
		return s.onCall(s.calls)
	}
	if s.err != nil {
		return Quote{}, s.err
	}
	q := s.quote
	q.FromAmount = amount
	q.ToAmount = amount * q.Rate
	return q, nil
}

func TestCachingRateProvider_CachesWithinTTL(t *testing.T) {
	primary := &stubProvider{quote: Quote{FromCurrency: "NGN", ToCurrency: "USDC", Rate: 0.0006}}
	clock := time.Now()
	cp := NewCachingRateProvider(primary, time.Minute)
	cp.now = func() time.Time { return clock }

	q1, err := cp.GetQuote(context.Background(), "NGN", "USDC", 1000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if q1.ToAmount != 0.6 {
		t.Fatalf("expected ToAmount 0.6, got %v", q1.ToAmount)
	}
	if primary.calls != 1 {
		t.Fatalf("expected 1 primary call, got %d", primary.calls)
	}

	// Second call within TTL — should hit cache, not the primary.
	clock = clock.Add(10 * time.Second)
	q2, err := cp.GetQuote(context.Background(), "NGN", "USDC", 2000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if primary.calls != 1 {
		t.Fatalf("expected primary to still have been called once (cache hit), got %d calls", primary.calls)
	}
	if q2.ToAmount != 2000*0.0006 {
		t.Fatalf("expected rescaled ToAmount %v, got %v", 2000*0.0006, q2.ToAmount)
	}
}

func TestCachingRateProvider_RefreshesAfterTTLExpires(t *testing.T) {
	primary := &stubProvider{quote: Quote{FromCurrency: "NGN", ToCurrency: "USDC", Rate: 0.0006}}
	clock := time.Now()
	cp := NewCachingRateProvider(primary, time.Minute)
	cp.now = func() time.Time { return clock }

	if _, err := cp.GetQuote(context.Background(), "NGN", "USDC", 1000); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	clock = clock.Add(2 * time.Minute) // past TTL
	if _, err := cp.GetQuote(context.Background(), "NGN", "USDC", 1000); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if primary.calls != 2 {
		t.Fatalf("expected primary to be called again after TTL expiry, got %d calls", primary.calls)
	}
}

func TestCachingRateProvider_FallsBackToStaleCacheOnProviderError(t *testing.T) {
	callCount := 0
	primary := &stubProvider{
		onCall: func(n int) (Quote, error) {
			callCount = n
			if n == 1 {
				return Quote{FromCurrency: "NGN", ToCurrency: "USDC", Rate: 0.0006, FromAmount: 1000, ToAmount: 0.6}, nil
			}
			return Quote{}, errors.New("upstream aggregator unavailable")
		},
	}
	clock := time.Now()
	cp := NewCachingRateProvider(primary, time.Minute)
	cp.now = func() time.Time { return clock }

	// Prime the cache.
	if _, err := cp.GetQuote(context.Background(), "NGN", "USDC", 1000); err != nil {
		t.Fatalf("unexpected error priming cache: %v", err)
	}

	// Move past TTL so the next call must go to the (now failing) primary.
	clock = clock.Add(2 * time.Minute)

	q, err := cp.GetQuote(context.Background(), "NGN", "USDC", 500)
	if err != nil {
		t.Fatalf("expected fallback to stale cache, got error: %v", err)
	}
	if callCount != 2 {
		t.Fatalf("expected primary to have been retried, call count = %d", callCount)
	}
	if q.Rate != 0.0006 {
		t.Fatalf("expected the stale cached rate 0.0006, got %v", q.Rate)
	}
	if q.ToAmount != 500*0.0006 {
		t.Fatalf("expected fallback quote rescaled to requested amount, got %v", q.ToAmount)
	}
}

func TestCachingRateProvider_ErrorsWhenProviderFailsAndNothingCached(t *testing.T) {
	primary := &stubProvider{err: errors.New("aggregator down")}
	cp := NewCachingRateProvider(primary, time.Minute)

	_, err := cp.GetQuote(context.Background(), "KES", "USDC", 100)
	if err == nil {
		t.Fatal("expected an error when the provider fails with no cached fallback available")
	}
}

func TestCachingRateProvider_CachesIndependentlyPerCurrencyPair(t *testing.T) {
	primary := &stubProvider{quote: Quote{Rate: 0.0006}}
	clock := time.Now()
	cp := NewCachingRateProvider(primary, time.Minute)
	cp.now = func() time.Time { return clock }

	if _, err := cp.GetQuote(context.Background(), "NGN", "USDC", 1000); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := cp.GetQuote(context.Background(), "KES", "USDC", 1000); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if primary.calls != 2 {
		t.Fatalf("expected a separate primary call per currency pair, got %d calls", primary.calls)
	}
}

func TestCachingRateProvider_NonPositiveTTLDefaultsInsteadOfDisablingCache(t *testing.T) {
	primary := &stubProvider{quote: Quote{Rate: 0.0006}}
	cp := NewCachingRateProvider(primary, 0)

	if cp.ttl <= 0 {
		t.Fatalf("expected a positive default TTL, got %v", cp.ttl)
	}
}
