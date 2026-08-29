package fx

import (
	"context"
	"time"

	"github.com/moistello/backend/internal/domain/yellowcard"
)

// ycClient is the subset of *yellowcard.Client this adapter needs, so tests
// can substitute a fake without standing up an HTTP client.
type ycClient interface {
	GetQuote(fromCurrency, toCurrency string, amount float64) (*yellowcard.Quote, error)
}

// YellowCardRateProvider adapts *yellowcard.Client to the RateProvider
// interface. Yellow Card's own client is unaware of `context.Context`
// cancellation today (its HTTP calls have no ctx-aware method), so this
// adapter does not yet honor ctx cancellation either — it's threaded
// through the interface so that changes only in this file.
type YellowCardRateProvider struct {
	client ycClient
}

func NewYellowCardRateProvider(client *yellowcard.Client) *YellowCardRateProvider {
	return &YellowCardRateProvider{client: client}
}

func (p *YellowCardRateProvider) GetQuote(_ context.Context, fromCurrency, toCurrency string, amount float64) (Quote, error) {
	q, err := p.client.GetQuote(fromCurrency, toCurrency, amount)
	if err != nil {
		return Quote{}, err
	}
	return Quote{
		QuoteID:       q.QuoteID,
		FromCurrency:  q.FromCurrency,
		ToCurrency:    q.ToCurrency,
		FromAmount:    q.FromAmount,
		ToAmount:      q.ToAmount,
		Rate:          q.Rate,
		Fee:           q.Fee,
		FeePercentage: q.FeePercentage,
		ExpiresAt:     q.ExpiresAt,
		FetchedAt:     time.Now().UTC(),
	}, nil
}
