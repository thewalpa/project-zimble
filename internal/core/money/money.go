// Package money is the integer money type: signed minor units (hundredths of
// the currency unit) in an int64, with overflow-checked arithmetic. Money is
// never a float.
package money

import (
	"errors"
	"fmt"
	"math"
	"strconv"
)

// Money is an amount in minor units; 100 minor units are one unit.
type Money int64

// MinorPerUnit is the number of minor units in one currency unit.
const MinorPerUnit = 100

// ErrOverflow: the result does not fit in an int64.
var ErrOverflow = errors.New("money: overflow")

// Units returns whole currency units as Money. It panics on overflow, so use
// it for constants only.
func Units(u int64) Money {
	if u > math.MaxInt64/MinorPerUnit || u < math.MinInt64/MinorPerUnit {
		panic(fmt.Sprintf("money: %d units overflow", u))
	}
	return Money(u * MinorPerUnit)
}

// Add returns m + o, or ErrOverflow.
func (m Money) Add(o Money) (Money, error) {
	if (o > 0 && m > math.MaxInt64-o) || (o < 0 && m < math.MinInt64-o) {
		return 0, fmt.Errorf("%w: %d + %d", ErrOverflow, m, o)
	}
	return m + o, nil
}

// Neg returns -m, or ErrOverflow for the most negative value.
func (m Money) Neg() (Money, error) {
	if m == math.MinInt64 {
		return 0, fmt.Errorf("%w: -(%d)", ErrOverflow, m)
	}
	return -m, nil
}

// Sum adds amounts in order, or returns ErrOverflow.
func Sum(ms ...Money) (Money, error) {
	var total Money
	for _, m := range ms {
		var err error
		if total, err = total.Add(m); err != nil {
			return 0, err
		}
	}
	return total, nil
}

// String formats m with thousands separators and two decimals, e.g.
// "-1,234,567.89".
func (m Money) String() string {
	neg := m < 0
	u := uint64(m)
	if neg {
		u = uint64(-(m + 1)) + 1 // safe for MinInt64
	}
	whole, frac := u/MinorPerUnit, u%MinorPerUnit
	digits := strconv.FormatUint(whole, 10)
	var out []byte
	if neg {
		out = append(out, '-')
	}
	for i, d := range []byte(digits) {
		if i > 0 && (len(digits)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, d)
	}
	return fmt.Sprintf("%s.%02d", out, frac)
}
