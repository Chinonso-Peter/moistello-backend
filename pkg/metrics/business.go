package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	ContributionsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "moistello",
			Name:      "contributions_total",
			Help:      "Total number of contribution transactions.",
		},
		[]string{"status", "currency", "method"},
	)

	ContributionVolumeTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "moistello",
			Name:      "contribution_volume_total",
			Help:      "Total volume of contributions.",
		},
		[]string{"currency"},
	)

	PayoutsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "moistello",
			Name:      "payouts_total",
			Help:      "Total number of payout transactions.",
		},
		[]string{"status", "currency", "method"},
	)

	PayoutVolumeTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "moistello",
			Name:      "payout_volume_total",
			Help:      "Total volume of payouts.",
		},
		[]string{"currency"},
	)

	UserRegistrationsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "moistello",
			Name:      "user_registrations_total",
			Help:      "Total number of user registrations.",
		},
		[]string{"method"},
	)

	ActiveUsersGauge = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "moistello",
			Name:      "active_users_current",
			Help:      "Current number of active users.",
		},
	)
)
