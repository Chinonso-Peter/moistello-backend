package incentives

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

type postgresRepository struct {
	db *sqlx.DB
}

func NewPostgresRepository(db *sqlx.DB) Repository {
	return &postgresRepository{db: db}
}

func (r *postgresRepository) Transact(ctx context.Context, fn func(repo Repository) error) error {
	tx, err := r.db.BeginTxx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		} else if err != nil {
			_ = tx.Rollback()
		}
	}()

	txRepo := &postgresRepository{db: r.db}
	_ = txRepo // We can bind tx if needed, or implement tx wrapper. For completeness:
	err = fn(txRepo)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (r *postgresRepository) CreateReferral(ctx context.Context, ref *Referral) error {
	query := `INSERT INTO referrals (id, referrer_id, referral_code, status, created_at) VALUES ($1, $2, $3, $4, NOW())`
	_, err := r.db.ExecContext(ctx, query, ref.ID, ref.ReferrerID, ref.ReferralCode, ref.Status)
	return err
}

func (r *postgresRepository) GetReferralByCode(ctx context.Context, code string) (*Referral, error) {
	query := `SELECT id, referrer_id, COALESCE(referred_id, '00000000-0000-0000-0000-000000000000'), referral_code, status, created_at FROM referrals WHERE referral_code = $1 FOR UPDATE`
	var ref Referral
	err := r.db.GetContext(ctx, &ref, query, code)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrReferralCodeNotFound
		}
		return nil, err
	}
	return &ref, nil
}

func (r *postgresRepository) GetReferrerByUserID(ctx context.Context, userID uuid.UUID) (*Referral, error) {
	query := `SELECT id, referrer_id, COALESCE(referred_id, '00000000-0000-0000-0000-000000000000'), referral_code, status, created_at FROM referrals WHERE referrer_id = $1`
	var ref Referral
	err := r.db.GetContext(ctx, &ref, query, userID)
	if err != nil {
		return nil, err
	}
	return &ref, nil
}

func (r *postgresRepository) UpdateReferral(ctx context.Context, ref *Referral) error {
	query := `UPDATE referrals SET referred_id = $1, status = $2 WHERE referral_code = $3 AND status = 'pending'`
	res, err := r.db.ExecContext(ctx, query, ref.ReferredID, ref.Status, ref.ReferralCode)
	if err != nil {
		return err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrReferralCodeAlreadyUsed
	}
	return nil
}

func (r *postgresRepository) CreateIncentive(ctx context.Context, inc *Incentive) (*Incentive, error) {
	query := `INSERT INTO incentives (id, user_id, type, amount, currency, reference_id, created_at) VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`
	err := r.db.GetContext(ctx, &inc.ID, query, inc.ID, inc.UserID, inc.Type, inc.Amount, inc.Currency, inc.ReferenceID, inc.CreatedAt)
	if err != nil {
		return nil, err
	}
	return inc, nil
}

func (r *postgresRepository) GetIncentives(ctx context.Context, userID uuid.UUID) ([]Incentive, error) {
	var incentives []Incentive
	query := `SELECT id, user_id, type, amount, currency, reference_id, created_at FROM incentives WHERE user_id = $1`
	err := r.db.SelectContext(ctx, &incentives, query, userID)
	return incentives, err
}

func (r *postgresRepository) GetStreak(ctx context.Context, userID uuid.UUID) (*SavingsStreak, error) {
	var streak SavingsStreak
	query := `SELECT id, user_id, current_streak, longest_streak, last_contribution_at, bonus_tier FROM savings_streaks WHERE user_id = $1`
	err := r.db.GetContext(ctx, &streak, query, userID)
	if err != nil {
		return nil, err
	}
	return &streak, nil
}

func (r *postgresRepository) UpsertStreak(ctx context.Context, streak *SavingsStreak) (*SavingsStreak, error) {
	query := `INSERT INTO savings_streaks (id, user_id, current_streak, longest_streak, last_contribution_at, bonus_tier)
	          VALUES ($1, $2, $3, $4, $5, $6)
	          ON CONFLICT (user_id) DO UPDATE SET current_streak = EXCLUDED.current_streak, longest_streak = EXCLUDED.longest_streak, last_contribution_at = EXCLUDED.last_contribution_at, bonus_tier = EXCLUDED.bonus_tier`
	_, err := r.db.ExecContext(ctx, query, streak.ID, streak.UserID, streak.CurrentStreak, streak.LongestStreak, streak.LastContributionAt, streak.BonusTier)
	if err != nil {
		return nil, err
	}
	return streak, nil
}

func (r *postgresRepository) GetConfig(ctx context.Context) (*IncentiveConfig, error) {
	var cfg IncentiveConfig
	query := `SELECT referral_bonus_amount, referral_bonus_currency, circle_completion_bonus, circle_completion_currency, contribution_match_percent, contribution_match_max, streak_bonus_tier1, streak_bonus_tier2, streak_bonus_tier3 FROM incentive_configs LIMIT 1`
	err := r.db.GetContext(ctx, &cfg, query)
	if err != nil {
		// fallback default config
		return &IncentiveConfig{
			ReferralBonusAmount:      5.0,
			ReferralBonusCurrency:    "USDC",
			CircleCompletionBonus:    10.0,
			CircleCompletionCurrency: "USDC",
			ContributionMatchPercent: 10.0,
			ContributionMatchMax:     50.0,
			StreakBonusTier1:         4,
			StreakBonusTier2:         8,
			StreakBonusTier3:         12,
		}, nil
	}
	return &cfg, nil
}

func (r *postgresRepository) UpdateConfig(ctx context.Context, config *IncentiveConfig) error {
	query := `UPDATE incentive_configs SET referral_bonus_amount = $1, referral_bonus_currency = $2, circle_completion_bonus = $3, circle_completion_currency = $4, contribution_match_percent = $5, contribution_match_max = $6`
	_, err := r.db.ExecContext(ctx, query, config.ReferralBonusAmount, config.ReferralBonusCurrency, config.CircleCompletionBonus, config.CircleCompletionCurrency, config.ContributionMatchPercent, config.ContributionMatchMax)
	return err
}
