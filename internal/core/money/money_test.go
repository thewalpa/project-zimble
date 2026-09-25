package money

import (
	"errors"
	"math"
	"testing"
)

func TestArithmeticChecksOverflow(t *testing.T) {
	if got, err := Money(150).Add(-250); err != nil || got != -100 {
		t.Fatalf("150 + -250 = %d, %v", got, err)
	}
	for name, f := range map[string]func() error{
		"max + 1": func() error { _, err := Money(math.MaxInt64).Add(1); return err },
		"min - 1": func() error { _, err := Money(math.MinInt64).Add(-1); return err },
		"-min":    func() error { _, err := Money(math.MinInt64).Neg(); return err },
		"sum":     func() error { _, err := Sum(math.MaxInt64/2+1, math.MaxInt64/2+1); return err },
	} {
		if err := f(); !errors.Is(err, ErrOverflow) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if s, err := Sum(Units(3), -50, 7); err != nil || s != 257 {
		t.Fatalf("Sum = %d, %v", s, err)
	}
	defer func() {
		if recover() == nil {
			t.Fatal("Units overflow did not panic")
		}
	}()
	Units(math.MaxInt64 / 10)
}

func TestString(t *testing.T) {
	for m, want := range map[Money]string{
		0: "0.00", 5: "0.05", 100: "1.00", -1: "-0.01", Units(1234567) + 89: "1,234,567.89",
		-Units(1000): "-1,000.00", math.MinInt64: "-92,233,720,368,547,758.08", math.MaxInt64: "92,233,720,368,547,758.07",
	} {
		if got := m.String(); got != want {
			t.Errorf("Money(%d) = %q, want %q", int64(m), got, want)
		}
	}
}
