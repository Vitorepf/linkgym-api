package auth

import (
	"strings"
	"unicode"
)

func NormalizePhone(raw string) (string, error) {
	var digits strings.Builder
	for _, r := range raw {
		if unicode.IsDigit(r) {
			digits.WriteRune(r)
		}
	}
	d := digits.String()
	switch {
	case len(d) == 11:
		return "+55" + d, nil
	case len(d) == 13 && strings.HasPrefix(d, "55"):
		return "+" + d, nil
	case len(d) == 10:
		return "+55" + d, nil
	default:
		return "", ErrPhoneInvalid
	}
}
