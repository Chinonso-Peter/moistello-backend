package contribution

import (
	"context"
	"github.com/moistello/backend/pkg/metrics"
)

type Service interface {
	Contribute(ctx context.Context, circleID, userID string, amount float64, currency, method string) error
}

type service struct {
	repo Repository
}

func NewService(repo Repository) Service {
	return &service{repo: repo}
}

func (s *service) Contribute(ctx context.Context, circleID, userID string, amount float64, currency, method string) error {
	err := s.repo.Create(ctx, circleID, userID, amount, currency, method)
	if err != nil {
		metrics.ContributionsTotal.WithLabelValues("failure", currency, method).Inc()
		return err
	}
	metrics.ContributionsTotal.WithLabelValues("success", currency, method).Inc()
	metrics.ContributionVolumeTotal.WithLabelValues(currency).Add(amount)
	return nil
}
