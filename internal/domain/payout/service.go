package payout

import (
	"context"
	"github.com/moistello/backend/pkg/metrics"
)

type Service interface {
	Distribute(ctx context.Context, circleID, recipientID string, amount float64, currency, method string) error
}

type service struct {
	repo Repository
}

func NewService(repo Repository) Service {
	return &service{repo: repo}
}

func (s *service) Distribute(ctx context.Context, circleID, recipientID string, amount float64, currency, method string) error {
	err := s.repo.Create(ctx, circleID, recipientID, amount, currency, method)
	if err != nil {
		metrics.PayoutsTotal.WithLabelValues("failure", currency, method).Inc()
		return err
	}
	metrics.PayoutsTotal.WithLabelValues("success", currency, method).Inc()
	metrics.PayoutVolumeTotal.WithLabelValues(currency).Add(amount)
	return nil
}
