package fx

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// CachingRateProvider wraps a primary RateProvider with a short-lived
// in-memory cache and a fallback to the last successfully fetched rate for
// a currency pair when the primary provider errors. This keeps the
// pricing/deposit/withdraw paths resilient to a transient aggregator outage
// instead of hard-failing every request the moment the upstream oracle
// hiccups.
type CachingRateProvider struct {
	primary RateProvider
	ttl     time.Duration
	now     func() time.Time

	mu    sync.Mutex
	cache map[string]cachedQuote
}

type cachedQuote struct {
	quote     Quote
	fetchedAt time.Time
}

// NewCachingRateProvider wraps primary with a cache of the given TTL. A
// non-positive ttl falls back to a sane default (30s) rather than disabling
// caching outright, since callers passing a zero value are far more likely
// to mean "use the default" than "cache forever" or "never cache."
func NewCachingRateProvider(primary RateProvider, ttl time.Duration) *CachingRateProvider {
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	return &CachingRateProvider{
		primary: primary,
		ttl:     ttl,
		now:     time.Now,
		cache:   make(map[string]cachedQuote),
	}
}

func cacheKey(from, to string) string {
	return from + ":" + to
}

// GetQuote returns a quote for `amount` of fromCurrency -> toCurrency.
//
// If a rate for this currency pair was fetched within the TTL, it's reused
// (rescaled to the requested amount) without calling the primary provider.
// Otherwise the primary provider is queried; on success the result is
// cached and returned. On failure, the last known-good cached rate for this
// pair is used regardless of its age — a stale rate is far better than a
// hard failure for a fiat onramp/offramp — and only surfaces an error when
// no cached rate exists at all.
func (p *CachingRateProvider) GetQuote(ctx context.Context, fromCurrency, toCurrency string, amount float64) (Quote, error) {
	key := cacheKey(fromCurrency, toCurrency)

	p.mu.Lock()
	cached, hasCached := p.cache[key]
	p.mu.Unlock()

	if hasCached && p.now().Sub(cached.fetchedAt) < p.ttl {
		return scaleQuote(cached.quote, amount), nil
	}

	fresh, err := p.primary.GetQuote(ctx, fromCurrency, toCurrency, amount)
	if err == nil {
		p.mu.Lock()
		p.cache[key] = cachedQuote{quote: fresh, fetchedAt: p.now()}
		p.mu.Unlock()
		return fresh, nil
	}

	if hasCached {
		return scaleQuote(cached.quote, amount), nil
	}

	return Quote{}, fmt.Errorf("rate provider failed and no cached rate available for %s->%s: %w", fromCurrency, toCurrency, err)
}

// scaleQuote reuses a cached quote's rate to compute FromAmount/ToAmount for
// a newly requested amount, since a cached quote was originally fetched for
// whatever amount was requested at the time it was cached. FetchedAt is
// left untouched so callers can tell how stale the underlying rate is. Fee
// is intentionally not rescaled — the provider's fee model (flat, tiered,
// percentage, or a mix) isn't something this cache can safely reconstruct,
// so Fee/FeePercentage stay as reported for the originally cached amount.
func scaleQuote(q Quote, amount float64) Quote {
	q.FromAmount = amount
	q.ToAmount = amount * q.Rate
	return q
}
