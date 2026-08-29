// Package fx abstracts fiat<->USDC rate sourcing behind a RateProvider
// interface so the pricing/deposit/withdraw paths don't depend directly on
// any single aggregator (issue #189 — Spec §6 requires a multi-currency
// oracle; previously only NGN/USDC was hardcoded via a direct Yellow Card
// client call).
package fx

import (
	"context"
	"time"
)

// Quote is a normalized FX quote between a fiat currency and USDC,
// independent of which underlying provider produced it.
type Quote struct {
	// QuoteID identifies this specific quote with the underlying provider,
	// when the provider issues one (e.g. to lock a rate for a subsequent
	// transaction). Empty when the provider doesn't support quote locking
	// or when a cached/fallback quote is being reused for a new amount.
	QuoteID       string  `json:"quoteId"`
	FromCurrency  string  `json:"fromCurrency"`
	ToCurrency    string  `json:"toCurrency"`
	FromAmount    float64 `json:"fromAmount"`
	ToAmount      float64 `json:"toAmount"`
	Rate          float64 `json:"rate"`
	Fee           float64 `json:"fee"`
	FeePercentage float64 `json:"feePercentage"`
	// ExpiresAt is the provider's own quote-validity deadline, when it
	// supplies one. Not to be confused with FetchedAt/cache TTL below.
	ExpiresAt string `json:"expiresAt"`
	// FetchedAt is when the underlying rate was obtained from the
	// provider. A quote served from cache keeps the original fetch time so
	// callers can tell how stale it is.
	FetchedAt time.Time `json:"fetchedAt"`
}

// RateProvider is the source-of-truth abstraction for fiat<->USDC exchange
// rates. Implementations may wrap a single aggregator (YellowCardRateProvider)
// or compose several (CachingRateProvider, or a future multi-source
// aggregator/failover provider) without pricing/deposit/withdraw call sites
// needing to change.
type RateProvider interface {
	// GetQuote returns a quote converting `amount` of fromCurrency into
	// toCurrency. Currency codes are the provider's native codes (ISO 4217
	// for fiat, e.g. "NGN", "KES"; "USDC" for the stablecoin leg).
	GetQuote(ctx context.Context, fromCurrency, toCurrency string, amount float64) (Quote, error)
}
