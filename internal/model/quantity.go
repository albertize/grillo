// SPDX-License-Identifier: Apache-2.0

package model

import (
	"fmt"
	"math/big"
	"strings"
)

// Millicores is a CPU quantity in thousandths of a core. Integer only.
type Millicores int64

// Bytes is a memory or capacity quantity in bytes. Integer only.
type Bytes int64

const (
	maxDecimals = 3
	// maxInt64 bounds parsed quantities so callers never see wrapped values.
	maxInt64 = int64(^uint64(0) >> 1)
)

// ParseMillicores parses a Kubernetes-style CPU quantity into millicores.
// It accepts cores ("1", "0.5", "1.25", up to three decimals) or millicores
// ("100m"). Negative values, extra precision, and overflow are rejected.
func ParseMillicores(s string) (Millicores, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, errQuantity("cpu", s)
	}
	if suffix, ok := strings.CutSuffix(s, "m"); ok {
		thousandths, err := parseThousandths(suffix)
		if err != nil {
			return 0, errQuantity("cpu", s)
		}
		if new(big.Int).Mod(thousandths, big.NewInt(1000)).Sign() != 0 {
			return 0, fmt.Errorf("cpu %q is more precise than one millicore", s)
		}
		milli := new(big.Int).Quo(thousandths, big.NewInt(1000))
		return millicoresFromBig(milli, s)
	}
	thousandths, err := parseThousandths(s)
	if err != nil {
		return 0, errQuantity("cpu", s)
	}
	return millicoresFromBig(thousandths, s)
}

// ParseBytes parses a Kubernetes-style memory quantity into bytes. It accepts a
// plain integer, decimal SI suffixes (k, M, G, T, P, E), or binary suffixes
// (Ki, Mi, Gi, Ti, Pi, Ei), with up to three decimals.
func ParseBytes(s string) (Bytes, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, errQuantity("memory", s)
	}
	number, multiplier, err := splitByteQuantity(s)
	if err != nil {
		return 0, err
	}
	thousandths, err := parseThousandths(number)
	if err != nil {
		return 0, errQuantity("memory", s)
	}
	// bytes = thousandths/1000 * multiplier, kept integral to avoid rounding.
	product := new(big.Int).Mul(thousandths, multiplier)
	if new(big.Int).Mod(product, big.NewInt(1000)).Sign() != 0 {
		return 0, fmt.Errorf("memory %q is not a whole number of bytes", s)
	}
	value := new(big.Int).Quo(product, big.NewInt(1000))
	if !value.IsInt64() || value.Int64() > maxInt64 {
		return 0, fmt.Errorf("memory %q overflows", s)
	}
	return Bytes(value.Int64()), nil
}

func splitByteQuantity(s string) (number string, multiplier *big.Int, err error) {
	suffixes := []struct {
		suffix string
		base   int64
		power  int
	}{
		{"Ki", 1024, 1}, {"Mi", 1024, 2}, {"Gi", 1024, 3}, {"Ti", 1024, 4}, {"Pi", 1024, 5}, {"Ei", 1024, 6},
		{"k", 1000, 1}, {"K", 1000, 1}, {"M", 1000, 2}, {"G", 1000, 3}, {"T", 1000, 4}, {"P", 1000, 5}, {"E", 1000, 6},
	}
	for _, entry := range suffixes {
		if strings.HasSuffix(s, entry.suffix) {
			return strings.TrimSuffix(s, entry.suffix), pow(entry.base, entry.power), nil
		}
	}
	// A trailing letter that is not a known suffix is an error, not a number.
	if last := s[len(s)-1]; last < '0' || last > '9' {
		return "", nil, errQuantity("memory", s)
	}
	return s, big.NewInt(1), nil
}

func pow(base int64, times int) *big.Int {
	result := big.NewInt(1)
	for i := 0; i < times; i++ {
		result.Mul(result, big.NewInt(base))
	}
	return result
}

// parseThousandths parses a non-negative decimal with at most three fractional
// digits into value*1000 as an exact integer.
func parseThousandths(s string) (*big.Int, error) {
	if s == "" {
		return nil, fmt.Errorf("empty number")
	}
	intPart, frac, _ := strings.Cut(s, ".")
	if intPart == "" {
		intPart = "0"
	}
	if len(frac) > maxDecimals {
		return nil, fmt.Errorf("more than %d decimal places", maxDecimals)
	}
	if err := allDigits(intPart); err != nil {
		return nil, err
	}
	if err := allDigits(frac); err != nil {
		return nil, err
	}
	for len(frac) < maxDecimals {
		frac += "0"
	}
	n, ok := new(big.Int).SetString(intPart+frac, 10)
	if !ok {
		return nil, fmt.Errorf("invalid number %q", s)
	}
	return n, nil
}

func allDigits(s string) error {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return fmt.Errorf("non-digit %q", s[i])
		}
	}
	return nil
}

func millicoresFromBig(n *big.Int, original string) (Millicores, error) {
	if !n.IsInt64() || n.Int64() > maxInt64 {
		return 0, fmt.Errorf("cpu %q overflows", original)
	}
	return Millicores(n.Int64()), nil
}

func errQuantity(kind, s string) error {
	return fmt.Errorf("invalid %s quantity %q", kind, s)
}

// String renders millicores in the canonical "<n>m" form.
func (m Millicores) String() string { return fmt.Sprintf("%dm", int64(m)) }

// String renders bytes as a decimal integer followed by "B".
func (b Bytes) String() string { return fmt.Sprintf("%dB", int64(b)) }
