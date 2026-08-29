package fx

import "testing"

func TestIsSupportedFiatCurrency(t *testing.T) {
	cases := []struct {
		code string
		want bool
	}{
		{"NGN", true},
		{"KES", true},
		{"GHS", true},
		{"ZAR", true},
		{"UGX", true},
		{"USD", false},
		{"ngn", false}, // case-sensitive: caller must normalize
		{"", false},
	}

	for _, tc := range cases {
		if got := IsSupportedFiatCurrency(tc.code); got != tc.want {
			t.Errorf("IsSupportedFiatCurrency(%q) = %v, want %v", tc.code, got, tc.want)
		}
	}
}
