// Package price parses Vietnamese đồng (VND) prices as shown by shop pages.
package price

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Plausible price range for the gear we watch. Anything outside it is almost
// certainly a parsing mistake (a SKU, a phone number, a monthly instalment).
const (
	MinVND int64 = 500_000
	MaxVND int64 = 200_000_000
)

// ErrOutOfRange is returned when a parsed price falls outside [MinVND, MaxVND].
var ErrOutOfRange = errors.New("price out of plausible range")

var (
	// numberRe finds the first number, including thousands separators.
	numberRe = regexp.MustCompile(`\d[\d.,]*`)
	// fractionRe matches a trailing decimal fraction such as ".00" in
	// "27490000.00" (JSON-LD). VND thousands groups always have 3 digits, so a
	// 1–2 digit tail can only be a fraction.
	fractionRe = regexp.MustCompile(`[.,]\d{1,2}$`)
)

// ParseVND parses a price string such as "27,490,000₫", "27.490.000 đ",
// "27490000" or "27490000.00".
//
// A string without any digits (e.g. "Liên hệ", call for price) yields 0 and no
// error: the shop lists the item but does not show a price.
func ParseVND(s string) (int64, error) {
	num := numberRe.FindString(s)
	if num == "" {
		return 0, nil
	}

	num = strings.TrimRight(num, ".,")
	num = fractionRe.ReplaceAllString(num, "")
	digits := strings.NewReplacer(".", "", ",", "").Replace(num)

	v, err := strconv.ParseInt(digits, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse price %q: %w", s, err)
	}
	if v < MinVND || v > MaxVND {
		return 0, fmt.Errorf("parse price %q: %d VND: %w", s, v, ErrOutOfRange)
	}

	return v, nil
}
