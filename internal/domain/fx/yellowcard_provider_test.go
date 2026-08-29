package fx

import (
	"context"
	"errors"
	"testing"

	"github.com/moistello/backend/internal/domain/yellowcard"
)

type fakeYcClient struct {
	quote          *yellowcard.Quote
	err            error
	gotFrom, gotTo string
	gotAmount      float64
}

func (f *fakeYcClient) GetQuote(fromCurrency, toCurrency string, amount float64) (*yellowcard.Quote, error) {
	f.gotFrom, f.gotTo, f.gotAmount = fromCurrency, toCurrency, amount
	if f.err != nil {
		return nil, f.err
	}
	return f.quote, nil
}

func TestYellowCardRateProvider_MapsQuoteFields(t *testing.T) {
	fake := &fakeYcClient{
		quote: &yellowcard.Quote{
			QuoteID:       "q-123",
			FromCurrency:  "KES",
			ToCurrency:    "USDC",
			FromAmount:    1000,
			ToAmount:      7.5,
			Rate:          0.0075,
			Fee:           0.05,
			FeePercentage: 1.5,
			ExpiresAt:     "2026-01-01T00:00:00Z",
		},
	}
	provider := &YellowCardRateProvider{client: fake}

	q, err := provider.GetQuote(context.Background(), "KES", "USDC", 1000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fake.gotFrom != "KES" || fake.gotTo != "USDC" || fake.gotAmount != 1000 {
		t.Fatalf("underlying client called with wrong args: from=%s to=%s amount=%v", fake.gotFrom, fake.gotTo, fake.gotAmount)
	}
	if q.Rate != 0.0075 || q.ToAmount != 7.5 || q.FeePercentage != 1.5 {
		t.Fatalf("quote fields not mapped correctly: %+v", q)
	}
	if q.QuoteID != "q-123" || q.ExpiresAt != "2026-01-01T00:00:00Z" {
		t.Fatalf("QuoteID/ExpiresAt not mapped correctly: %+v", q)
	}
	if q.FetchedAt.IsZero() {
		t.Fatal("expected FetchedAt to be set")
	}
}

func TestYellowCardRateProvider_PropagatesUnderlyingError(t *testing.T) {
	fake := &fakeYcClient{err: errors.New("yellow card API error")}
	provider := &YellowCardRateProvider{client: fake}

	_, err := provider.GetQuote(context.Background(), "NGN", "USDC", 1000)
	if err == nil {
		t.Fatal("expected error to propagate")
	}
}
