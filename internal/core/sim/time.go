// Package sim provides the simulation runtime: logical game time, the career
// calendar and the deterministic task scheduler. It knows nothing about
// football; task kinds and payloads are defined by the application.
package sim

import (
	"fmt"
	"time"
)

// GameInstant is a logical time: whole minutes since the career epoch.
// It never refers to wall-clock time.
type GameInstant int64

// Duration is a span of logical minutes.
type Duration int64

const (
	Minute Duration = 1
	Hour            = 60 * Minute
	Day             = 24 * Hour
	Week            = 7 * Day
)

// MaxInstant bounds the supported range [-MaxInstant, MaxInstant], about
// 950 years either side of the epoch.
const MaxInstant GameInstant = 500_000_000

// Supported epoch years. With MaxInstant this keeps all civil times within
// years 1..9999.
const (
	MinEpochYear = 1900
	MaxEpochYear = 2999
)

func (i GameInstant) Valid() bool { return i >= -MaxInstant && i <= MaxInstant }

// Add returns i+d, or an error if the result leaves the supported range.
func (i GameInstant) Add(d Duration) (GameInstant, error) {
	if d > Duration(2*MaxInstant) || d < -Duration(2*MaxInstant) {
		return 0, fmt.Errorf("sim: duration %d out of range", d)
	}
	r := i + GameInstant(d)
	if !i.Valid() || !r.Valid() {
		return 0, fmt.Errorf("sim: instant %d + %d outside supported range", i, d)
	}
	return r, nil
}

// CivilTime is a calendar date and time of day in UTC. The game uses UTC
// throughout: no time zones and no daylight saving. Competition-local
// presentation, if ever needed, is an adapter concern.
type CivilTime struct {
	Year, Month, Day, Hour, Minute int
}

func (c CivilTime) String() string {
	return fmt.Sprintf("%04d-%02d-%02d %02d:%02d UTC", c.Year, c.Month, c.Day, c.Hour, c.Minute)
}

func (c CivilTime) utc() time.Time {
	return time.Date(c.Year, time.Month(c.Month), c.Day, c.Hour, c.Minute, 0, 0, time.UTC)
}

// normalized reports whether c names a real UTC minute (no overflowing
// fields such as February 30 or minute 60).
func (c CivilTime) normalized() bool {
	return c.Month >= 1 && c.Month <= 12 && civilOf(c.utc()) == c
}

func civilOf(t time.Time) CivilTime {
	t = t.UTC()
	return CivilTime{t.Year(), int(t.Month()), t.Day(), t.Hour(), t.Minute()}
}

// Calendar converts between game instants and UTC civil times for one
// career. Its epoch (instant 0) is fixed when the career is created.
type Calendar struct {
	epoch     CivilTime
	epochUnix int64 // seconds
}

// NewCalendar returns a calendar whose instant 0 is epoch.
func NewCalendar(epoch CivilTime) (Calendar, error) {
	if !epoch.normalized() {
		return Calendar{}, fmt.Errorf("sim: epoch %+v is not a valid UTC minute", epoch)
	}
	if epoch.Year < MinEpochYear || epoch.Year > MaxEpochYear {
		return Calendar{}, fmt.Errorf("sim: epoch year %d outside %d..%d", epoch.Year, MinEpochYear, MaxEpochYear)
	}
	return Calendar{epoch: epoch, epochUnix: epoch.utc().Unix()}, nil
}

func (c Calendar) Epoch() CivilTime { return c.epoch }

// Valid reports whether c was built by NewCalendar.
func (c Calendar) Valid() bool { return c.epoch.Year != 0 }

// Civil converts an instant to UTC civil time.
func (c Calendar) Civil(i GameInstant) (CivilTime, error) {
	if !c.Valid() {
		return CivilTime{}, fmt.Errorf("sim: zero calendar")
	}
	if !i.Valid() {
		return CivilTime{}, fmt.Errorf("sim: instant %d outside supported range", i)
	}
	return civilOf(time.Unix(c.epochUnix+int64(i)*60, 0)), nil
}

// Instant converts a UTC civil time to an instant.
func (c Calendar) Instant(t CivilTime) (GameInstant, error) {
	if !c.Valid() {
		return 0, fmt.Errorf("sim: zero calendar")
	}
	if !t.normalized() {
		return 0, fmt.Errorf("sim: %+v is not a valid UTC minute", t)
	}
	i := GameInstant((t.utc().Unix() - c.epochUnix) / 60)
	if !i.Valid() {
		return 0, fmt.Errorf("sim: %s outside supported range", t)
	}
	return i, nil
}

// Format renders an instant as "Sat 2025-08-09 15:00 UTC", or the raw
// instant if it is out of range.
func (c Calendar) Format(i GameInstant) string {
	civil, err := c.Civil(i)
	if err != nil {
		return fmt.Sprintf("instant %d", i)
	}
	return civil.utc().Weekday().String()[:3] + " " + civil.String()
}
