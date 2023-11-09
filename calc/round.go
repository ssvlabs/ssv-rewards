package calc

import (
	"encoding/json"
	"time"
)

const (
	RoundTimeFormat = "2006-01"
)

type Round time.Time

func NewRound(year int, month time.Month) Round {
	return Round(time.Date(year, month, 1, 0, 0, 0, 0, time.UTC))
}

func (r Round) String() string {
	return time.Time(r).Format(RoundTimeFormat)
}

func (r Round) MarshalJSON() ([]byte, error) {
	return json.Marshal(r.String())
}

func (r *Round) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	t, err := time.ParseInLocation(RoundTimeFormat, s, time.UTC)
	if err != nil {
		return err
	}
	*r = Round(t)
	return nil
}
