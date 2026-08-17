package beaconcha

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/carlmjohnson/requests"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv-rewards/pkg/beacon"
	"github.com/bloxapp/ssv-rewards/pkg/sync/httpretry"
	"github.com/bloxapp/ssv-rewards/pkg/sync/performance"
)

const (
	ProviderType performance.ProviderType = "beaconcha"
	// First day of the Incentivized Mainnet Program (IMP), corresponds to 2023-07-01
	// Used as the start_day parameter when querying the Beaconcha.in API for validator stats.
	startDay = "941"

	// memCacheRetainDays bounds how many days of stats are retained in memory per
	// validator, starting at the most recently requested day. Sync processes days in
	// strictly increasing order, so days before the requested one can never be read
	// again, and days further ahead than the sync window are not needed yet. Keeping
	// the full history instead (1,100+ days since IMP start, across every synced
	// validator) requires tens of GiB of heap at current mainnet scale.
	// 45 days covers a monthly sync round in a single file parse per validator;
	// longer (re)sync windows re-parse each validator's cache file once per 45 days.
	memCacheRetainDays = 45
)

type Client struct {
	endpoint string
	apiKey   string
	ticker   *time.Ticker
	cacheDir string

	memCache map[phase0.ValidatorIndex]map[time.Time]dailyData
	cacheMu  sync.RWMutex
}

func New(endpoint string, apiKey string, requestsPerMinute float64, cacheDir string) (*Client, error) {
	err := os.MkdirAll(cacheDir, 0755)
	if err != nil {
		return nil, fmt.Errorf("failed to create cache directory: %w", err)
	} // ensure cache directory exists

	return &Client{
		endpoint: endpoint,
		apiKey:   apiKey,
		ticker:   time.NewTicker(time.Duration(float64(time.Minute) / requestsPerMinute)),
		cacheDir: cacheDir,
		memCache: make(map[phase0.ValidatorIndex]map[time.Time]dailyData),
	}, nil
}

func (m *Client) Type() performance.ProviderType {
	return ProviderType
}

func (m *Client) ValidatorPerformance(
	ctx context.Context,
	logger *zap.Logger,
	spec beacon.Spec,
	day time.Time,
	fromEpoch, toEpoch, activationEpoch, exitEpoch phase0.Epoch,
	index phase0.ValidatorIndex,
) (*performance.ValidatorPerformance, error) {
	dayKey := day.UTC().Truncate(24 * time.Hour)

	// 1. Try memory cache
	m.cacheMu.RLock()
	if byDay, ok := m.memCache[index]; ok {
		if d, ok := byDay[dayKey]; ok {
			m.cacheMu.RUnlock()
			return convertToPerformance(spec, d, fromEpoch, toEpoch, activationEpoch, exitEpoch), nil
		}
	}
	m.cacheMu.RUnlock()

	// 2. Try file cache
	cachedItem, err := m.loadCache(index)
	if err == nil {
		// Use cache only if it was fetched *after* the requested day + 48h
		if cachedItem.Time.After(dayKey.Add(48 * time.Hour)) {
			// Load the retention window into the memory cache AND check for the
			// target dayKey in a single pass. Replacing the entry (rather than
			// merging) also drops days that have already been processed.
			byDay, d, found := retainWindow(cachedItem.Data, dayKey)

			m.cacheMu.Lock()
			m.memCache[index] = byDay
			m.cacheMu.Unlock()

			if found {
				return convertToPerformance(spec, d, fromEpoch, toEpoch, activationEpoch, exitEpoch), nil
			}
			return nil, nil
		} else {
			logger.Info("BEACONCHA cache is stale", zap.String("index", fmt.Sprintf("%d", index)))
		}
	}

	// 3. Fetch fresh from API
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	// Rate-limiting
	select {
	case <-m.ticker.C:
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	var resp response
	err = requests.URL(m.endpoint).
		Client(httpretry.Client).
		Pathf("/api/v1/validator/stats/%d", index).
		Param("apikey", m.apiKey).
		Param("start_day", startDay).
		ToJSON(&resp).
		Fetch(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch validator performance: %w", err)
	}
	if resp.Status != "OK" {
		return nil, fmt.Errorf("failed to fetch validator performance: %s", resp.Status)
	}

	// Save fresh data to cache immediately
	newCache := cacheItem{
		Time: time.Now(),
		Data: resp.Data,
	}
	if saveErr := m.saveCache(index, newCache); saveErr != nil {
		logger.Error("failed to save cache", zap.Error(saveErr))
	}

	// Update in-memory cache with the retention window only.
	byDay, d, found := retainWindow(resp.Data, dayKey)

	m.cacheMu.Lock()
	m.memCache[index] = byDay
	m.cacheMu.Unlock()

	if found {
		return convertToPerformance(spec, d, fromEpoch, toEpoch, activationEpoch, exitEpoch), nil
	}

	// Requested day not found even after fetching fresh data
	return nil, nil
}

// retainWindow filters daily stats down to [dayKey, dayKey+memCacheRetainDays)
// keyed by day, and returns the entry for dayKey itself, with found reporting
// whether that day was present.
func retainWindow(data []dailyData, dayKey time.Time) (byDay map[time.Time]dailyData, day dailyData, found bool) {
	windowEnd := dayKey.AddDate(0, 0, memCacheRetainDays)
	byDay = make(map[time.Time]dailyData)
	for _, d := range data {
		k := d.DayStart.UTC().Truncate(24 * time.Hour)
		if k.Before(dayKey) || !k.Before(windowEnd) {
			continue
		}
		byDay[k] = d
		if k.Equal(dayKey) {
			day = d
			found = true
		}
	}
	return byDay, day, found
}

func (m *Client) cacheFilePath(index phase0.ValidatorIndex) string {
	return filepath.Join(m.cacheDir, fmt.Sprintf("%d.json", index))
}

func (m *Client) loadCache(index phase0.ValidatorIndex) (*cacheItem, error) {
	data, err := os.ReadFile(m.cacheFilePath(index))
	if err != nil {
		return nil, err
	}
	var item cacheItem
	err = json.Unmarshal(data, &item)
	return &item, err
}

func (m *Client) saveCache(index phase0.ValidatorIndex, item cacheItem) error {
	data, err := json.MarshalIndent(item, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.cacheFilePath(index), data, 0644)
}

type cacheItem struct {
	Time time.Time   `json:"time"`
	Data []dailyData `json:"data"`
}

func convertToPerformance(
	spec beacon.Spec,
	d dailyData,
	fromEpoch, toEpoch, activationEpoch, exitEpoch phase0.Epoch,
) *performance.ValidatorPerformance {
	activeEpochs := deriveActiveEpochs(spec, fromEpoch, toEpoch, activationEpoch, exitEpoch)

	p := &performance.ValidatorPerformance{
		Attestations: performance.DutyPerformance{
			Assigned: int16(activeEpochs),
			Executed: int16(activeEpochs) - int16(d.MissedAttestations),
			Missed:   int16(d.MissedAttestations),
		},
		Proposals: performance.DutyPerformance{
			Assigned: int16(d.ProposedBlocks + d.MissedBlocks),
			Executed: int16(d.ProposedBlocks),
			Missed:   int16(d.MissedBlocks),
		},
		SyncCommittee: performance.DutyPerformance{
			Assigned: int16(d.ParticipatedSync + d.MissedSync),
			Executed: int16(d.ParticipatedSync),
			Missed:   int16(d.MissedSync),
		},
		EndEffectiveBalance: int64(d.EndEffectiveBalance),
	}
	if p.Attestations.Assigned > 0 {
		p.AttestationRate = float32(p.Attestations.Executed) / float32(p.Attestations.Assigned)
	}
	return p
}

type response struct {
	Status string      `json:"status"`
	Data   []dailyData `json:"data"`
}

type dailyData struct {
	Day                   int       `json:"day"`
	AttesterSlashings     int       `json:"attester_slashings"`
	DayEnd                time.Time `json:"day_end"`
	DayStart              time.Time `json:"day_start"`
	Deposits              uint64    `json:"deposits"`
	DepositsAmount        uint64    `json:"deposits_amount"`
	EndBalance            uint64    `json:"end_balance"`
	EndEffectiveBalance   uint64    `json:"end_effective_balance"`
	MaxBalance            uint64    `json:"max_balance"`
	MaxEffectiveBalance   uint64    `json:"max_effective_balance"`
	MinBalance            uint64    `json:"min_balance"`
	MinEffectiveBalance   uint64    `json:"min_effective_balance"`
	MissedAttestations    int       `json:"missed_attestations"`
	MissedBlocks          int       `json:"missed_blocks"`
	MissedSync            int       `json:"missed_sync"`
	OrphanedAttestations  int       `json:"orphaned_attestations"`
	OrphanedBlocks        int       `json:"orphaned_blocks"`
	OrphanedSync          int       `json:"orphaned_sync"`
	ParticipatedSync      int       `json:"participated_sync"`
	ProposedBlocks        int       `json:"proposed_blocks"`
	ProposerSlashings     int       `json:"proposer_slashings"`
	StartBalance          uint64    `json:"start_balance"`
	StartEffectiveBalance uint64    `json:"start_effective_balance"`
	ValidatorIndex        int       `json:"validatorindex"`
	Withdrawals           uint64    `json:"withdrawals"`
	WithdrawalsAmount     uint64    `json:"withdrawals_amount"`
}

func deriveActiveEpochs(
	spec beacon.Spec, fromEpoch, toEpoch, activationEpoch, exitEpoch phase0.Epoch,
) phase0.Epoch {
	activeEpochs := toEpoch - fromEpoch + 1
	if activationEpoch > fromEpoch {
		if activationEpoch > toEpoch {
			activeEpochs = 0
		} else {
			activeEpochs -= activationEpoch - fromEpoch
		}
	}
	if exitEpoch != spec.FarFutureEpoch && exitEpoch <= toEpoch {
		if exitEpoch <= fromEpoch {
			activeEpochs = 0
		} else {
			activeEpochs -= toEpoch - exitEpoch + 1
		}
	}
	return activeEpochs
}
