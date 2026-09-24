package sim

import (
	"testing"
	"time"
)

var epoch = CivilTime{2025, 7, 1, 0, 0}

func calendar(t *testing.T) Calendar {
	t.Helper()
	c, err := NewCalendar(epoch)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestCalendarKnownValues(t *testing.T) {
	c := calendar(t)
	cases := map[GameInstant]CivilTime{
		0:                  epoch,
		1:                  {2025, 7, 1, 0, 1},
		-1:                 {2025, 6, 30, 23, 59},
		GameInstant(Day):   {2025, 7, 2, 0, 0},
		57_060:             {2025, 8, 9, 15, 0}, // 39 days + 15 hours
		1_401_120:          {2028, 2, 29, 0, 0}, // leap day
		GameInstant(-Week): {2025, 6, 24, 0, 0},
	}
	for i, want := range cases {
		got, err := c.Civil(i)
		if err != nil || got != want {
			t.Errorf("Civil(%d) = %v, %v; want %v", i, got, err, want)
		}
		back, err := c.Instant(want)
		if err != nil || back != i {
			t.Errorf("Instant(%v) = %d, %v; want %d", want, back, err, i)
		}
	}
	if got := c.Format(57_060); got != "Sat 2025-08-09 15:00 UTC" {
		t.Errorf("Format = %q", got)
	}
}

func TestCalendarRoundTripsSupportedRange(t *testing.T) {
	c := calendar(t)
	check := func(i GameInstant) {
		civil, err := c.Civil(i)
		if err != nil {
			t.Fatalf("Civil(%d): %v", i, err)
		}
		back, err := c.Instant(civil)
		if err != nil || back != i {
			t.Fatalf("round trip %d -> %v -> %d, %v", i, civil, back, err)
		}
	}
	for _, i := range []GameInstant{0, 1, -1, MaxInstant, -MaxInstant, MaxInstant - 1, -MaxInstant + 1} {
		check(i)
	}
	// Deterministic sweep: a prime stride visits minutes at every hour and
	// weekday offset across the whole range.
	for i := -MaxInstant; i <= MaxInstant; i += 999_983 {
		check(i)
	}
	// Every minute of a leap-year February and the day after.
	start, _ := c.Instant(CivilTime{2028, 2, 1, 0, 0})
	for i := start; i < start+GameInstant(30*Day); i++ {
		check(i)
	}
}

func TestCalendarIgnoresLocalTimeZone(t *testing.T) {
	c := calendar(t)
	want, _ := c.Civil(57_060)
	saved := time.Local
	t.Cleanup(func() { time.Local = saved })
	time.Local = time.FixedZone("Test+05:30", 5*3600+1800)
	if got, _ := c.Civil(57_060); got != want {
		t.Fatalf("Civil changed with time.Local: %v, want %v", got, want)
	}
	if got, _ := c.Instant(want); got != 57_060 {
		t.Fatalf("Instant changed with time.Local: %d", got)
	}
}

func TestCalendarRejectsInvalidInput(t *testing.T) {
	for _, e := range []CivilTime{{}, {1899, 12, 31, 0, 0}, {3000, 1, 1, 0, 0}, {2025, 2, 30, 0, 0}, {2025, 13, 1, 0, 0}, {2025, 1, 1, 24, 0}} {
		if _, err := NewCalendar(e); err == nil {
			t.Errorf("NewCalendar(%+v) succeeded", e)
		}
	}
	c := calendar(t)
	for _, i := range []GameInstant{MaxInstant + 1, -MaxInstant - 1} {
		if _, err := c.Civil(i); err == nil {
			t.Errorf("Civil(%d) succeeded", i)
		}
	}
	for _, ct := range []CivilTime{{2025, 2, 29, 0, 0}, {2025, 7, 1, 0, 60}, {2025, 0, 1, 0, 0}, {9999, 1, 1, 0, 0}} {
		if _, err := c.Instant(ct); err == nil {
			t.Errorf("Instant(%+v) succeeded", ct)
		}
	}
	if _, err := (Calendar{}).Civil(0); err == nil {
		t.Error("zero calendar converted an instant")
	}
}

func TestInstantAddChecksRange(t *testing.T) {
	if got, err := GameInstant(10).Add(Week); err != nil || got != 10+GameInstant(Week) {
		t.Fatalf("Add = %d, %v", got, err)
	}
	for _, c := range []struct {
		i GameInstant
		d Duration
	}{{MaxInstant, 1}, {-MaxInstant, -1}, {0, 1 << 62}, {MaxInstant + 1, 0}} {
		if _, err := c.i.Add(c.d); err == nil {
			t.Errorf("%d.Add(%d) succeeded", c.i, c.d)
		}
	}
}
