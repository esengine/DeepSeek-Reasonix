// Package billing models wallet balances, fixed-point money, and cost quotes.
// It is a utility-layer package (stdlib only).
package billing

import (
	"fmt"
	"math"
	"math/big"
	"strings"
)

// Money is a currency-tagged amount carried as a decimal string on the wire.
// Arithmetic uses fixed-point int64 units (1e9 fractional digits).
type Money struct {
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
}

// amountScale is 1e9: enough for per-token rates down to ~1e-15 currency units
// when aggregated over typical request sizes.
const amountScale int64 = 1_000_000_000

// Amount is a fixed-point currency magnitude in 1e-9 units of the currency.
type Amount int64

// Zero is the zero amount.
const Zero Amount = 0

// NewAmountFromFloat converts a float64 amount to fixed-point. Prefer ParseAmount
// or NewAmountFromString for values that originate as decimals.
func NewAmountFromFloat(v float64) Amount {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return Zero
	}
	r := new(big.Rat).SetFloat64(v)
	if r == nil {
		return Zero
	}
	return amountFromRat(r)
}

// NewAmountFromString parses a decimal amount string.
func NewAmountFromString(s string) (Amount, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Zero, nil
	}
	r := new(big.Rat)
	if _, ok := r.SetString(s); !ok {
		return Zero, fmt.Errorf("billing: invalid amount %q", s)
	}
	return amountFromRat(r), nil
}

// ParseAmount is an alias for NewAmountFromString.
func ParseAmount(s string) (Amount, error) { return NewAmountFromString(s) }

func amountFromRat(r *big.Rat) Amount {
	if r == nil {
		return Zero
	}
	scaled := new(big.Rat).Mul(r, big.NewRat(amountScale, 1))
	// Round half away from zero.
	num := new(big.Int).Set(scaled.Num())
	den := new(big.Int).Set(scaled.Denom())
	half := new(big.Int).Rsh(den, 1)
	if scaled.Sign() >= 0 {
		num.Add(num, half)
	} else {
		num.Sub(num, half)
	}
	quot := new(big.Int).Quo(num, den)
	if !quot.IsInt64() {
		if scaled.Sign() >= 0 {
			return Amount(math.MaxInt64)
		}
		return Amount(math.MinInt64)
	}
	return Amount(quot.Int64())
}

// String returns a trimmed decimal representation.
func (a Amount) String() string {
	neg := a < 0
	v := int64(a)
	if neg {
		v = -v
	}
	whole := v / amountScale
	frac := v % amountScale
	s := fmt.Sprintf("%d.%09d", whole, frac)
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	if s == "" || s == "-" {
		s = "0"
	}
	if neg && s != "0" {
		return "-" + s
	}
	return s
}

// Float64 approximates the amount for legacy float fields only.
func (a Amount) Float64() float64 {
	return float64(a) / float64(amountScale)
}

// Add returns a+b. Same-currency only at the Money layer.
func (a Amount) Add(b Amount) Amount {
	sum := int64(a) + int64(b)
	// Saturate on overflow rather than wrap.
	if (b > 0 && sum < int64(a)) || (b < 0 && sum > int64(a)) {
		if b > 0 {
			return Amount(math.MaxInt64)
		}
		return Amount(math.MinInt64)
	}
	return Amount(sum)
}

// MulRate multiplies by a float rate for a pricing calculation.
func (a Amount) MulRate(rate float64) Amount {
	if a == 0 || rate == 0 || math.IsNaN(rate) || math.IsInf(rate, 0) {
		return Zero
	}
	r := new(big.Rat).SetFloat64(rate)
	if r == nil {
		return Zero
	}
	ar := big.NewRat(int64(a), amountScale)
	return amountFromRat(ar.Mul(ar, r))
}

// MoneyOf builds a Money value.
func MoneyOf(amount Amount, currency string) Money {
	return Money{Amount: amount.String(), Currency: NormalizeCurrency(currency)}
}

// ParseMoney parses amount+currency into fixed-point Money.
func ParseMoney(amount, currency string) (Money, Amount, error) {
	a, err := NewAmountFromString(amount)
	if err != nil {
		return Money{}, Zero, err
	}
	cur := NormalizeCurrency(currency)
	return MoneyOf(a, cur), a, nil
}

// AmountValue returns the fixed-point amount, or zero on parse error.
func (m Money) AmountValue() Amount {
	a, err := NewAmountFromString(m.Amount)
	if err != nil {
		return Zero
	}
	return a
}

// IsZero reports whether the money amount is empty or zero.
func (m Money) IsZero() bool {
	return m.AmountValue() == Zero
}

// Float64 is a legacy adapter. Prefer Amount strings for aggregation.
func (m Money) Float64() float64 { return m.AmountValue().Float64() }

// NormalizeCurrency maps symbols and aliases to ISO-4217 codes when known.
// Unknown three-letter codes pass through uppercased; empty stays empty.
func NormalizeCurrency(currency string) string {
	value := strings.TrimSpace(currency)
	if value == "" {
		return ""
	}
	switch strings.ToUpper(value) {
	case "CNY", "RMB", "CNH", "YUAN", "RENMINBI":
		return "CNY"
	case "USD", "US$", "DOLLAR", "DOLLARS":
		return "USD"
	case "EUR", "EURO", "EUROS":
		return "EUR"
	case "GBP", "POUND", "POUNDS", "STERLING":
		return "GBP"
	case "JPY", "YEN":
		return "JPY"
	}
	switch value {
	case "¥", "￥":
		return "CNY"
	case "$":
		return "USD"
	case "€":
		return "EUR"
	case "£":
		return "GBP"
	}
	if len(value) == 3 {
		allAlpha := true
		for _, r := range value {
			if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') {
				allAlpha = false
				break
			}
		}
		if allAlpha {
			return strings.ToUpper(value)
		}
	}
	return value
}

// CurrencySymbol returns a compact display symbol for an ISO code or symbol.
func CurrencySymbol(currency string) string {
	switch NormalizeCurrency(currency) {
	case "CNY", "JPY":
		return "¥"
	case "USD":
		return "$"
	case "EUR":
		return "€"
	case "GBP":
		return "£"
	case "":
		return "¥"
	default:
		code := NormalizeCurrency(currency)
		if len(code) == 3 {
			return code + " "
		}
		return currency
	}
}

// SameCurrency reports whether two codes normalize equal and non-empty.
func SameCurrency(a, b string) bool {
	na, nb := NormalizeCurrency(a), NormalizeCurrency(b)
	return na != "" && na == nb
}

// AddMoney adds two Money values of the same currency. Mixed currencies error.
func AddMoney(a, b Money) (Money, error) {
	ca, cb := NormalizeCurrency(a.Currency), NormalizeCurrency(b.Currency)
	if ca == "" {
		ca = cb
	}
	if cb == "" {
		cb = ca
	}
	if ca == "" && cb == "" {
		return Money{Amount: "0", Currency: ""}, nil
	}
	if ca != cb {
		return Money{}, fmt.Errorf("billing: cannot add %s and %s", ca, cb)
	}
	sum := a.AmountValue().Add(b.AmountValue())
	return MoneyOf(sum, ca), nil
}
