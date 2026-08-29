package fx

// SupportedFiatCurrencies is the set of fiat currency codes the pricing
// path will quote against USDC. Yellow Card supports a wider set of
// corridors than this; this list is deliberately conservative and should
// only grow alongside real per-currency transfer caps (see
// config.YellowCardConfig.CurrencyCaps) — quoting a currency doesn't imply
// deposits/withdrawals are enabled for it yet.
var SupportedFiatCurrencies = map[string]bool{
	"NGN": true,
	"GHS": true,
	"KES": true,
	"ZAR": true,
	"UGX": true,
}

// IsSupportedFiatCurrency reports whether code is a currency the pricing
// path can quote. Comparison is case-sensitive by design — currency codes
// arriving from API requests should already be normalized (upper-cased) by
// the caller so a mismatch here signals genuinely invalid input.
func IsSupportedFiatCurrency(code string) bool {
	return SupportedFiatCurrencies[code]
}
