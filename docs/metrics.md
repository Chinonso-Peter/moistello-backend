# Business Metrics Documentation

Moistello exports Prometheus business metrics for monitoring contributions, payouts, user registrations, and active users.

## Metrics Catalog

- `moistello_contributions_total{status, currency, method}`: Counter of contribution transactions.
- `moistello_contribution_volume_total{currency}`: Counter of total contribution volume.
- `moistello_payouts_total{status, currency, method}`: Counter of payout transactions.
- `moistello_payout_volume_total{currency}`: Counter of total payout volume.
- `moistello_user_registrations_total{method}`: Counter of user sign-ups.
- `moistello_active_users_current`: Gauge representing active platform users.

## Useful Prometheus Queries

- Contribution Rate (per sec):
  `rate(moistello_contributions_total[5m])`
- Payout Volume (sum by currency):
  `sum(rate(moistello_payout_volume_total[1h])) by (currency)`
