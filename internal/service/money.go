package service

import (
	"strings"

	"github.com/kiflet/backend/pkg/payments"
)

// Money is whole santim everywhere inside (docs/architecture.md, section
// 5): these read and write it where people type birr.

// ParseBirr reads an amount of birr as a person types it ("2,875.50",
// "-100", at most two decimals) as santim. Empty is 0.
func ParseBirr(s string) (int64, error) {
	s = strings.ReplaceAll(strings.TrimSpace(s), ",", "")
	if s == "" {
		return 0, nil
	}
	rest, negative := strings.CutPrefix(s, "-")
	santim, err := payments.ParseDecimal(rest)
	if negative {
		santim = -santim
	}
	return santim, err
}

// BirrText writes santim as ParseBirr reads it, for a form: "2875.50",
// "-100.00", or "" for none.
func BirrText(santim int64) string {
	switch {
	case santim == 0:
		return ""
	case santim < 0:
		return "-" + payments.Decimal(-santim)
	default:
		return payments.Decimal(santim)
	}
}
