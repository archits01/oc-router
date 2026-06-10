package payment

import (
	"github.com/shopspring/decimal"
)

func CalculatePayAmount(rechargeAmount float64, feeRate float64) string {
	return CalculatePayAmountForCurrency(rechargeAmount, feeRate, DefaultPaymentCurrency)
}

// CalculatePayAmountForCurrency
func CalculatePayAmountForCurrency(rechargeAmount float64, feeRate float64, currency string) string {
	fractionDigits := int32(CurrencyMaxFractionDigits(currency))
	amount := decimal.NewFromFloat(rechargeAmount)
	if feeRate <= 0 {
		return amount.StringFixed(fractionDigits)
	}
	rate := decimal.NewFromFloat(feeRate)
	fee := amount.Mul(rate).Div(decimal.NewFromInt(100)).RoundUp(fractionDigits)
	return amount.Add(fee).StringFixed(fractionDigits)
}
