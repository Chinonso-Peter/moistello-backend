package swap

import (
	"context"
	"time"
)

type Repository interface {
	CreateSwapOffer(ctx context.Context, offer *SwapOffer) error
	GetSwapOfferByID(ctx context.Context, id string) (*SwapOffer, error)
	UpdateSwapOfferStatus(ctx context.Context, id string, status SwapOfferStatus, transactionHash *string) error
	CompareAndSwapStatus(ctx context.Context, id string, expectedStatus, newStatus SwapOfferStatus, transactionHash *string) (bool, error)
	ListUserSwapOffers(ctx context.Context, userID string, filter SwapHistoryFilter) ([]SwapOffer, int, error)
	ListCircleSwapOffers(ctx context.Context, circleID string, filter SwapHistoryFilter) ([]SwapOffer, int, error)
	ListExpiredCreatedOffers(ctx context.Context, now time.Time) ([]SwapOffer, error)
}
