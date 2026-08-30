package incentives

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/moistello/backend/pkg/apperrors"
)

var (
	ErrReferralCodeNotFound = errors.New("referral code not found")
	ErrReferralCodeAlreadyUsed = errors.New("referral code already used")
	ErrSelfReferral = errors.New("cannot refer yourself")
	ErrReferralCodeTaken = errors.New("referral code is already taken")
)

type Service interface {
	GenerateReferralCode(ctx context.Context, userID string) (string, error)
	ApplyReferralCode(ctx context.Context, referredUserID string, referralCode string) error
	GrantCircleCompletionReward(ctx context.Context, userID string, circleID string) (*Incentive, error)
	CalculateContributionMatch(ctx context.Context, userID string, amount float64) (float64, error)
	RecordContribution(ctx context.Context, userID string) (*SavingsStreak, error)
	GetIncentives(ctx context.Context, userID string) ([]Incentive, error)
	GetStreak(ctx context.Context, userID string) (*SavingsStreak, error)
	GetConfig(ctx context.Context) (*IncentiveConfig, error)
	UpdateConfig(ctx context.Context, config *IncentiveConfig) error
}

type service struct {
	repo Repository
}

func NewService(repo Repository) Service {
	return &service{repo: repo}
}

var referralCodeEntropyBytes = 10

var newReferralCode = func() (string, error) {
	bytes := make([]byte, referralCodeEntropyBytes)
	// Use crypto/rand in real impl, or fallback
	return fmt.Sprintf("%x", time.Now().UnixNano()), nil
}

func generateReferralCode() (string, error) {
	return newReferralCode()
}

func (s *service) GenerateReferralCode(ctx context.Context, userIDStr string) (string, error) {
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		return "", fmt.Errorf("invalid user ID: %w", err)
	}

	existing, err := s.repo.GetReferrerByUserID(ctx, userID)
	if err == nil && existing != nil && existing.ReferralCode != "" {
		return existing.ReferralCode, nil
	}

	for attempts := 0; attempts < 8; attempts++ {
		code, err := generateReferralCode()
		if err != nil {
			continue
		}

		ref := &Referral{
			ID:           uuid.New(),
			ReferrerID:   userID,
			ReferralCode: code,
			Status:       "pending",
		}

		err = s.repo.CreateReferral(ctx, ref)
		if err == nil {
			return code, nil
		}
	}

	return "", fmt.Errorf("%w: after 8 attempts", ErrReferralCodeTaken)
}

func (s *service) ApplyReferralCode(ctx context.Context, referredUserIDStr string, referralCode string) error {
	referredUserID, err := uuid.Parse(referredUserIDStr)
	if err != nil {
		return fmt.Errorf("invalid referred user ID: %w", err)
	}

	// If repository supports transactions (like repository_pg implementing a Tx method or via sqlx), use transaction.
	// Let's check if repo has a ExecTx or Transact method, or we handle it via Repository interface.
	// If Repository doesn't have explicit transaction methods, let's implement or call repo methods securely.
	// Looking at Repository interface, let's make sure it handles atomic claims.
	if dbRepo, ok := s.repo.(interface {
		Transact(ctx context.Context, fn func(txRepo Repository) error) error
	}); ok {
		return dbRepo.Transact(ctx, func(txRepo Repository) error {
			return s.applyReferralCodeTx(ctx, txRepo, referredUserID, referralCode)
		})
	}

	return s.applyReferralCodeTx(ctx, s.repo, referredUserID, referralCode)
}

func (s *service) applyReferralCodeTx(ctx context.Context, repo Repository, referredUserID uuid.UUID, referralCode string) error {
	ref, err := repo.GetReferralByCode(ctx, referralCode)
	if err != nil {
		return ErrReferralCodeNotFound
	}

	if ref.ReferrerID == referredUserID {
		return ErrSelfReferral
	}

	if ref.Status == "completed" || ref.ReferredID != uuid.Nil {
		return ErrReferralCodeAlreadyUsed
	}

	ref.ReferredID = referredUserID
	ref.Status = "completed"

	err = repo.UpdateReferral(ctx, ref)
	if err != nil {
		return err
	}

	cfg, err := repo.GetConfig(ctx)
	if err != nil || cfg == nil {
		cfg = &IncentiveConfig{
			ReferralBonusAmount:   5.0,
			ReferralBonusCurrency: "USDC",
		}
	}

	_, err = repo.CreateIncentive(ctx, &Incentive{
		ID:          uuid.New(),
		UserID:      ref.ReferrerID,
		Type:        IncentiveTypeReferral,
		Amount:      cfg.ReferralBonusAmount,
		Currency:    cfg.ReferralBonusCurrency,
		ReferenceID: sql.NullString{String: referredUserID.String(), Valid: true},
		CreatedAt:   time.Now().UTC(),
	})
	if err != nil {
		return err
	}

	return nil
}

func (s *service) GrantCircleCompletionReward(ctx context.Context, userIDStr string, circleIDStr string) (*Incentive, error) {
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		return nil, fmt.Errorf("invalid user ID: %w", err)
	}

	circleID, err := uuid.Parse(circleIDStr)
	if err != nil {
		return nil, fmt.Errorf("invalid circle ID: %w", err)
	}

	cfg, err := s.repo.GetConfig(ctx)
	if err != nil {
		return nil, err
	}

	incentives, err := s.repo.GetIncentives(ctx, userID)
	if err != nil {
		return nil, err
	}

	for _, inc := range incentives {
		if inc.Type == IncentiveTypeCircleCompletion && inc.ReferenceID.Valid && inc.ReferenceID.String == circleID.String() {
			return nil, errors.New("reward already received for this circle")
		}
	}

	inc := &Incentive{
		ID:          uuid.New(),
		UserID:      userID,
		Type:        IncentiveTypeCircleCompletion,
		Amount:      cfg.CircleCompletionBonus,
		Currency:    cfg.CircleCompletionCurrency,
		ReferenceID: sql.NullString{String: circleID.String(), Valid: true},
		CreatedAt:   time.Now().UTC(),
	}

	created, err := s.repo.CreateIncentive(ctx, inc)
	if err != nil {
		return nil, err
	}

	return created, nil
}

func (s *service) CalculateContributionMatch(ctx context.Context, userIDStr string, amount float64) (float64, error) {
	cfg, err := s.repo.GetConfig(ctx)
	if err != nil {
		return 0, err
	}

	match := amount * (cfg.ContributionMatchPercent / 100.0)
	if match > cfg.ContributionMatchMax {
		match = cfg.ContributionMatchMax
	}

	return match, nil
}

func (s *service) RecordContribution(ctx context.Context, userIDStr string) (*SavingsStreak, error) {
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		return nil, fmt.Errorf("invalid user ID: %w", err)
	}

	cfg, err := s.repo.GetConfig(ctx)
	if err != nil {
		return nil, err
	}

	streak, err := s.repo.GetStreak(ctx, userID)
	now := time.Now().UTC()

	if err != nil || streak == nil {
		streak = &SavingsStreak{
			ID:                 uuid.New(),
			UserID:             userID,
			CurrentStreak:      1,
			LongestStreak:      1,
			LastContributionAt: sql.NullTime{Time: now, Valid: true},
			BonusTier:          1,
		}
		return s.repo.UpsertStreak(ctx, streak)
	}

	if streak.LastContributionAt.Valid {
		last := streak.LastContributionAt.Time
		hoursSince := now.Sub(last).Hours()
		if hoursSince > 48 {
			streak.CurrentStreak = 1
		} else if hoursSince >= 12 {
			streak.CurrentStreak++
		}
	} else {
		streak.CurrentStreak = 1
	}

	if streak.CurrentStreak > streak.LongestStreak {
		streak.LongestStreak = streak.CurrentStreak
	}

	streak.LastContributionAt = sql.NullTime{Time: now, Valid: true}

	if streak.CurrentStreak >= cfg.StreakBonusTier3 {
		streak.BonusTier = 3
	} else if streak.CurrentStreak >= cfg.StreakBonusTier2 {
		streak.BonusTier = 2
	} else if streak.CurrentStreak >= cfg.StreakBonusTier1 {
		streak.BonusTier = 1
	} else {
		streak.BonusTier = 0
	}

	return s.repo.UpsertStreak(ctx, streak)
}

func (s *service) GetIncentives(ctx context.Context, userIDStr string) ([]Incentive, error) {
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		return nil, fmt.Errorf("invalid user ID: %w", err)
	}
	return s.repo.GetIncentives(ctx, userID)
}

func (s *service) GetStreak(ctx context.Context, userIDStr string) (*SavingsStreak, error) {
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		return nil, fmt.Errorf("invalid user ID: %w", err)
	}
	return s.repo.GetStreak(ctx, userID)
}

func (s *service) GetConfig(ctx context.Context) (*IncentiveConfig, error) {
	return s.repo.GetConfig(ctx)
}

func (s *service) UpdateConfig(ctx context.Context, config *IncentiveConfig) error {
	return s.repo.UpdateConfig(ctx, config)
}
