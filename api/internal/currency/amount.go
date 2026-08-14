package currency

import (
	"errors"
	"math/big"
	"strings"
)

var ErrRateUnavailable = errors.New("exchange rate is unavailable")

func ParseRate(value string) (*big.Rat, error) {
	value = strings.TrimSpace(value)
	if value == "" || strings.HasPrefix(value, "-") || strings.HasPrefix(value, "+") {
		return nil, errors.New("exchange rate is invalid")
	}
	rate, ok := new(big.Rat).SetString(value)
	if !ok || rate.Sign() <= 0 || rate.Num().BitLen() > 256 || rate.Denom().BitLen() > 256 {
		return nil, errors.New("exchange rate is invalid")
	}
	return rate, nil
}

func power10(exponent int) (*big.Int, error) {
	if exponent < 0 || exponent > 6 {
		return nil, errors.New("currency minor unit is invalid")
	}
	return new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(exponent)), nil), nil
}

func roundHalfUp(value *big.Rat) (*big.Int, error) {
	if value == nil || value.Sign() < 0 {
		return nil, errors.New("money amount is invalid")
	}
	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(value.Num(), value.Denom(), remainder)
	if new(big.Int).Lsh(remainder, 1).Cmp(value.Denom()) >= 0 {
		quotient.Add(quotient, big.NewInt(1))
	}
	if !quotient.IsInt64() {
		return nil, errors.New("converted money amount overflows")
	}
	return quotient, nil
}

// ConvertWithMarkup performs one exact rational operation and rounds only the
// final target minor-unit amount. This ordering produces 1 USD × 7.0267 CNY ×
// 150% = 10.54005 CNY -> 1054 fen, not an intermediate-rounded 1055 fen.
func ConvertWithMarkup(amount int64, sourceMinorUnit, targetMinorUnit int, rateDecimal string, markupBasisPoint int) (int64, error) {
	if amount < 0 || markupBasisPoint < -10_000 || markupBasisPoint > 1_000_000 {
		return 0, errors.New("money conversion input is invalid")
	}
	rate, err := ParseRate(rateDecimal)
	if err != nil {
		return 0, err
	}
	sourceScale, err := power10(sourceMinorUnit)
	if err != nil {
		return 0, err
	}
	targetScale, err := power10(targetMinorUnit)
	if err != nil {
		return 0, err
	}
	value := new(big.Rat).SetInt64(amount)
	value.Quo(value, new(big.Rat).SetInt(sourceScale))
	value.Mul(value, rate)
	value.Mul(value, new(big.Rat).SetInt(targetScale))
	value.Mul(value, new(big.Rat).SetFrac(big.NewInt(int64(10_000+markupBasisPoint)), big.NewInt(10_000)))
	rounded, err := roundHalfUp(value)
	if err != nil {
		return 0, err
	}
	return rounded.Int64(), nil
}

func Convert(amount int64, sourceMinorUnit, targetMinorUnit int, rateDecimal string) (int64, error) {
	return ConvertWithMarkup(amount, sourceMinorUnit, targetMinorUnit, rateDecimal, 0)
}
