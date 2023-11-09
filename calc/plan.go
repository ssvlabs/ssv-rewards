package calc

import (
	"errors"
	"sort"
	"time"
)

var DefaultRewardPlan = RewardPlan{
	From: NewRound(2023, time.October),
	To:   NewRound(2024, time.September),
	Tiers: RewardTiers{
		{MaxParticipants: 2000, MonthlyReward: 7.1},
		{MaxParticipants: 5000, MonthlyReward: 5.68},
		{MaxParticipants: 10000, MonthlyReward: 4.26},
		{MaxParticipants: 15000, MonthlyReward: 2.84},
		{MaxParticipants: 30000, MonthlyReward: 1.42},
	},
}

type RewardTier struct {
	MaxParticipants int
	MonthlyReward   float64
}

type RewardTiers []RewardTier

func (t RewardTiers) Len() int           { return len(t) }
func (t RewardTiers) Less(i, j int) bool { return t[i].MaxParticipants < t[j].MaxParticipants }
func (t RewardTiers) Swap(i, j int)      { t[i], t[j] = t[j], t[i] }

func (t RewardTiers) Tier(participants int) (*RewardTier, error) {
	if participants <= 0 {
		return nil, errors.New("participants must be positive")
	}
	if len(t) == 0 {
		return nil, errors.New("no reward tiers")
	}
	if !sort.IsSorted(t) {
		sort.Sort(t)
	}
	for _, tier := range t {
		if participants <= tier.MaxParticipants {
			return &tier, nil
		}
	}
	return &t[len(t)-1], nil
}

type RewardPlan struct {
	From  Round
	To    Round
	Tiers RewardTiers
}
