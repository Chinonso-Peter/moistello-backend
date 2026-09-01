package incentives

import (
	"context"
	"database/sql"
	"errors"
	"sync"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

type Repository interface {
	CreateReferral(ctx context.Context, ref *Referral) error
	GetReferralByCode(ctx context.Context, code string) (*Referral, error)
	GetReferrerByUserID(ctx context.Context, userID uuid.UUID) (*Referral, error)
	UpdateReferral(ctx context.Context, ref *Referral) error
	CreateIncentive(ctx context.Context, inc *Incentive) (*Incentive, error)
	GetIncentives(ctx context.Context, userID uuid.UUID) ([]Incentive, error)
	GetStreak(ctx context.Context, userID uuid.UUID) (*SavingsStreak, error)
	UpsertStreak(ctx context.Context, streak *SavingsStreak) (*SavingsStreak, error)
	GetConfig(ctx context.Context) (*IncentiveConfig, error)
	UpdateConfig(ctx context.Context, config *IncentiveConfig) error
	Transact(ctx context.Context, fn func(repo Repository) error) error
}

type mockRepository struct {
	mu                   sync.Mutex
	referralCodeMap      map[string]*Referral
	referrerUserMap      map[uuid.UUID]*Referral
	createdIncentives    []Incentive
	userIncentives       []Incentive
	streak               *SavingsStreak
	config               *IncentiveConfig
	updatedReferralStatus string
}

func newMockRepository() *mockRepository {
	return &mockRepository{
		referralCodeMap: make(map[string]*Referral),
		referrerUserMap: make(map[uuid.UUID]*Referral),
		config: &IncentiveConfig{
			ReferralBonusAmount:      5.0,
			ReferralBonusCurrency:    "USDC",
			CircleCompletionBonus:    10.0,
			CircleCompletionCurrency: "USDC",
			ContributionMatchPercent: 10.0,
			ContributionMatchMax:     50.0,
		},
	}
}

func (m *mockRepository) CreateReferral(ctx context.Context, ref *Referral) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.referralCodeMap[ref.ReferralCode]; exists {
		return errors.New("referral code taken")
	}
	m.referralCodeMap[ref.ReferralCode] = ref
	m.referrerUserMap[ref.ReferrerID] = ref
	return nil
}

func (m *mockRepository) GetReferralByCode(ctx context.Context, code string) (*Referral, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ref, exists := m.referralCodeMap[code]
	if !exists {
		return nil, errors.New("not found")
	}
	copy := *ref
	return &copy, nil
}

func (m *mockRepository) GetReferrerByUserID(ctx context.Context, userID uuid.UUID) (*Referral, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ref, exists := m.referrerUserMap[userID]
	if !exists {
		return nil, errors.New("not found")
	}
	copy := *ref
	return &copy, nil
}

func (m *mockRepository) UpdateReferral(ctx context.Context, ref *Referral) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	existing, exists := m.referralCodeMap[ref.ReferralCode]
	if !exists {
		return errors.New("not found")
	}
	if existing.Status == "completed" || existing.ReferredID != uuid.Nil {
		return ErrReferralCodeAlreadyUsed
	}
	existing.ReferredID = ref.ReferredID
	existing.Status = ref.Status
	m.updatedReferralStatus = ref.Status
	return nil
}

func (m *mockRepository) CreateIncentive(ctx context.Context, inc *Incentive) (*Incentive, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.createdIncentives = append(m.createdIncentives, *inc)
	m.userIncentives = append(m.userIncentives, *inc)
	copy := *inc
	return &copy, nil
}

func (m *mockRepository) GetIncentives(ctx context.Context, userID uuid.UUID) ([]Incentive, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var res []Incentive
	for _, inc := range m.userIncentives {
		if inc.UserID == userID {
			res = append(res, inc)
		}
	}
	return res, nil
}

func (m *mockRepository) GetStreak(ctx context.Context, userID uuid.UUID) (*SavingsStreak, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.streak != nil && m.streak.UserID == userID {
		copy := *m.streak
		return &copy, nil
	}
	return nil, errors.New("not found")
}

func (m *mockRepository) UpsertStreak(ctx context.Context, streak *SavingsStreak) (*SavingsStreak, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.streak = streak
	copy := *streak
	return &copy, nil
}

func (m *mockRepository) GetConfig(ctx context.Context) (*IncentiveConfig, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.config, nil
}

func (m *mockRepository) UpdateConfig(ctx context.Context, config *IncentiveConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.config = config
	return nil
}

func (m *mockRepository) Transact(ctx context.Context, fn func(repo Repository) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return fn(m)
}
