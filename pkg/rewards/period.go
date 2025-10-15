package rewards

import (
	"encoding/json"
	"fmt"
	"time"
)

const PeriodTimeFormat = "2006-01"

type Period time.Time

func NewPeriod(year int, month time.Month) Period {
	return Period(time.Date(year, month, 1, 0, 0, 0, 0, time.UTC))
}

func PeriodAt(t time.Time) Period {
	utc := t.UTC()
	return Period(time.Date(utc.Year(), utc.Month(), 1, 0, 0, 0, 0, time.UTC))
}

func ParsePeriod(s string) (Period, error) {
	t, err := time.ParseInLocation(PeriodTimeFormat, s, time.UTC)
	if err != nil {
		return Period{}, err
	}
	if !t.Equal(time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)) {
		return Period{}, fmt.Errorf("period must only specify year and month (e.g. 2006-01)")
	}
	return Period(t), nil
}

func (r Period) FirstDay() time.Time {
	return time.Date(time.Time(r).Year(), time.Time(r).Month(), 1, 0, 0, 0, 0, time.UTC)
}

func (r Period) LastDay() time.Time {
	return r.FirstDay().AddDate(0, 1, -1)
}

func (r Period) Days() int {
	return r.LastDay().Day()
}

func (r Period) String() string {
	return time.Time(r).Format(PeriodTimeFormat)
}

func (r Period) MarshalText() ([]byte, error) {
	return []byte(r.String()), nil
}

func (r *Period) UnmarshalText(data []byte) error {
	p, err := ParsePeriod(string(data))
	if err != nil {
		return err
	}
	*r = p
	return nil
}

func (r Period) MarshalJSON() ([]byte, error) {
	return json.Marshal(r.String())
}

func (r *Period) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	return r.UnmarshalText([]byte(s))
}

// Before reports whether the period r is before p2
func (r Period) Before(p2 Period) bool {
	return time.Time(r).Before(time.Time(p2))
}

// After reports whether the period r is after p2
func (r Period) After(p2 Period) bool {
	return time.Time(r).After(time.Time(p2))
}

// Equal reports whether the period r is equal to p2
func (r Period) Equal(p2 Period) bool {
	return time.Time(r).Equal(time.Time(p2))
}

// IsZero reports whether the period is the zero value
func (r Period) IsZero() bool {
	return time.Time(r).IsZero()
}
