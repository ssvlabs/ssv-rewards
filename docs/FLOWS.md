# SSV Rewards — Execution Flows

This document is the **implementation verification checklist** for ssv-rewards. It describes every pipeline flow with preconditions, step-by-step state mutations, postconditions, and invariants. For design intent, reward mechanics, and accounting formulas, see [SPEC.md](./SPEC.md).

| Document | Purpose |
|---|---|
| **SPEC.md** | Design intent · rules · formulas · invariants · source of truth |
| **FLOWS.md** (this file) | Step-by-step execution · preconditions · state mutations · test checklist |

## Table of Contents

1. [Sync Pipeline](#1-sync-pipeline)
   - [Contract Event Sync](#11-contract-event-sync)
   - [Validator Event Replay & Reconciliation](#12-validator-event-replay--reconciliation)
   - [Cluster Migration Handling](#13--cluster-migration-handling)
   - [Post-Upgrade ValidatorAdded](#14--post-upgrade-validatoradded)
   - [Validator Beacon Enrichment](#15-validator-beacon-enrichment)
   - [Performance Sync](#16-performance-sync)
2. [Calculation Pipeline](#2-calculation-pipeline)
   - [Plan Loading & Validation](#21-plan-loading--validation)
   - [Round Completeness Determination](#22-round-completeness-determination)
   - [Redirect Setup](#23-redirect-setup-per-round)
   - [Legacy Round Processing](#24-legacy-round-processing-pre-cutoff)
   - [Modern Round Processing](#25-modern-round-processing-post-cutoff-no-migration)
   - [Migration Round Processing](#26--migration-round-processing-post-cutoff-with-staking_upgrade)
   - [Network Fee Accumulation](#27-network-fee-accumulation)
3. [Export Pipeline](#3-export-pipeline)
   - [Per-Round Outputs](#31-per-round-outputs)
   - [Cross-Round Totals](#32-cross-round-totals)
   - [Exclusion Export](#33-exclusion-export)
   - [Inputs Archive](#34-inputs-archive)
4. [Merkleization](#4-merkleization)
5. [Invariants](#5-invariants)
6. [Reproducibility](#6-reproducibility)

## Sync Modes

The sync command supports two modes:

- **Fresh** (`--fresh`): Drops the entire PostgreSQL schema (`DROP SCHEMA public CASCADE; CREATE SCHEMA public`), recreates all tables from `schema.sql`, truncates SSV-related tables (`validators`, `validator_events`, `validator_performances`), and removes the data directory. Optional `--keep-cache` preserves the `.cache` subdirectory to avoid re-fetching from external APIs.
- **Fresh SSV** (`--fresh-ssv`): Same truncation and data-dir cleanup as `--fresh`, but does not drop the schema — preserves `contract_events` rows and `state` (however, `contract_events.error` values are cleared). Also respects `--keep-cache`.
- **Incremental** (default): Resumes contract event sync from `state.highest_block_number + 1`. Replays validator events from the last block processed by the SSV node KV storage onward (not all events). Skips performance days already present in `validator_performances`.

---

## 1. Sync Pipeline

### 1.1 Contract Event Sync

#### Preconditions
- Execution endpoint reachable and synced
- For fresh: `fromBlock` = `network.RegistrySyncOffset`
- For incremental: `fromBlock` = `state.highest_block_number + 1`
- `toBlock` defaults to the finalized block number; if `--to-block` is specified, it must be <= finalized (hard error otherwise)

#### Steps
1. Query finalized block number from execution client via `eth_getBlockByNumber("finalized")`
2. Determine range: `[fromBlock, min(toBlock, finalized)]`; error if `fromBlock > toBlock`
3. Call `el.FetchLogs(ctx, fromBlock, toBlock)` which streams `BlockLogs` grouped by block number
4. For each `BlockLogs` batch received on the channel:
   a. Fetch block header to obtain `blockTime` (only if the batch contains logs)
   b. Begin a database transaction
   c. For each log: parse via SSV's `EventParser.EventByID` + `ParseEvent`; if parsing succeeds, `event_name` is set to the ABI event name; if `EventByID` fails, the row is still inserted with an empty `event_name`; unknown parse results (known ABI event but `ErrUnknownEvent`) are stored with the ABI name but JSON `null` as `raw_event` (the column is `JSONB NOT NULL`, so JSON `null` satisfies the constraint)
   d. Derive `slot` from `blockTime` using `spec.SlotAt(blockTime)`
   e. Insert `contract_events` row with `block_number`, `block_hash`, `block_time`, `slot`, `transaction_hash`, `transaction_index`, `log_index`, `raw_log` (JSON), `raw_event` (JSON), `event_name`
   f. Update `state.highest_block_number` to the current block number
   g. Commit transaction
5. After all blocks processed, check the error channel for fetch failures

#### Postconditions
- All SSV contract logs in `[fromBlock, toBlock]` stored in `contract_events`
- `state.highest_block_number` = last processed block
- Each block's events are inserted atomically with the state update

---

### 1.2 Validator Event Replay & Reconciliation

#### Preconditions
- `contract_events` table populated (from 1.1)
- SSV node KV storage opened at `<dataDir>/ssv-node-storage`

#### Steps
1. Determine `lastProcessedBlock` from SSV node storage; if found, add 1 (resume after last processed); if not found, use `network.RegistrySyncOffset`
2. Load all `contract_events` with `block_number >= lastProcessedBlock` (post-increment), ordered by `(block_number, log_index)`
3. Unmarshal each event's `raw_log` JSON back into `types.Log`
4. Pack logs into `BlockLogs` groups using `executionclient.PackLogs`
5. Feed the `BlockLogs` channel to SSV's `EventHandler.HandleBlockEventsStream`
6. For each `EventTrace` emitted by the handler (processed in `recordHandledEvents`):
   a. Update the corresponding `contract_events` row with `error` (if the handler returned one)
   b. Fetch the full `contract_events` row for block time and slot data
   c. If `eventTrace.Error != nil` and log topic matches `ClusterMigratedToETHTopic`: delegate to `handleClusterMigration` (see 1.3), then clear the error and set `event_name` to `ClusterMigratedToETH`
   d. If `eventTrace.Error != nil` for any other event: skip (continue)
   e. Determine event type from the trace's typed event:
      - `ContractValidatorAdded`: `activated = true`, single pubkey; if `isPostStakingUpgrade` returns true, compute `addDay` (see 1.4)
      - `ContractValidatorRemoved`: `activated = false`, single pubkey
      - `ContractClusterLiquidated`: `activated = false`, look up all pubkeys by cluster ID from node storage
      - `ContractClusterReactivated`: `activated = true`, look up all pubkeys by cluster ID from node storage
   f. For each pubkey: upsert `validators` row (public_key, active); insert `validator_events` row
   g. If `addedPostStakingUpgrade` is set: `UPDATE validators SET migration_day = addDay WHERE public_key = ? AND migration_day IS NULL`
7. **Reconciliation**: Load all `validator_events` ordered by `(block_number, log_index)`, replay activated/deactivated to reconstruct the active validator set
8. Compare reconstructed set against `nodeStorage.Shares().List()` (non-liquidated shares)
9. If any missing or extra validators: hard error with details logged

#### Postconditions
- `validator_events` contains full lifecycle for all validators
- `validators` table reflects current active state
- Active set from replayed events matches EventHandler's internal set exactly

---

### 1.3 Cluster Migration Handling

#### Preconditions
- Log topic matches `ClusterMigratedToETHTopic` (the keccak256 of the event signature)
- Log has at least 2 topics
- Note: this flow runs whenever the topic matches, regardless of whether `staking_upgrade` is configured

#### Steps
1. Parse `owner` from `Topics[1]` (indexed address, padded to 32 bytes)
2. ABI-decode non-indexed data using the `ClusterMigratedToETH` ABI: `operatorIds (uint64[])`, `ethDeposited (uint256)`, `ssvRefunded (uint256)`, `effectiveBalance (uint32)`, `cluster (tuple)`
3. Extract `validatorCount` from the decoded cluster tuple's `ValidatorCount` field
4. Compute cluster ID: `ssvtypes.ComputeClusterIDHash(owner.Bytes(), operatorIds)`
5. In-memory dedup check: if `clusterIDStr` already in `migratedClusters` map, log a warning and return (skip)
6. Record `clusterIDStr` in `migratedClusters` map
7. Look up affected validators: `nodeStorage.Shares().List(nil, registrystorage.ByClusterID(clusterID))`
8. Validate: `len(shares)` must equal `validatorCount`; if both are zero, warn and continue; if mismatch, hard error suggesting `sync --fresh --keep-cache`
9. Compute `migrationDay = contractEvent.BlockTime.UTC().Truncate(24 * time.Hour)`
10. Begin database transaction
11. For each share (validator):
    a. `UPDATE validators SET migration_day = migrationDay WHERE public_key = ? AND migration_day IS NULL`
    b. `INSERT validator_events` with `event_name = 'ClusterMigratedToETH'`, `activated = true`, owner address lowercase without `0x` prefix
12. Commit transaction

#### Postconditions
- All validators in the migrated cluster have `migration_day` set (unless already set from a previous sync run — IS NULL guard)
- `validator_events` contains a `ClusterMigratedToETH` record for each validator in the cluster
- `contract_events.error` cleared and `event_name` set to `ClusterMigratedToETH`
- Cluster ID recorded in `migratedClusters` map for in-memory dedup

---

### 1.4 Post-Upgrade ValidatorAdded

#### Preconditions
- `ContractValidatorAdded` event processed normally by EventHandler (no error)
- `isPostStakingUpgrade` returns true: `blockNumber > upgrade.Block` OR (`blockNumber == upgrade.Block` AND `logIndex >= upgrade.LogIndex`)

#### Steps
1. Compute `addDay = contractEvent.BlockTime.UTC().Truncate(24 * time.Hour)`
2. After upserting the validator and inserting the validator event:
   `UPDATE validators SET migration_day = addDay WHERE public_key = ? AND migration_day IS NULL`

#### Postconditions
- Validators added after the `staking_upgrade` boundary have `migration_day` set to their add day
- IS NULL guard prevents overwriting if already set (e.g., from a prior `ClusterMigratedToETH` event)
- These validators will appear in the ETH tree from their first day

---

### 1.5 Validator Beacon Enrichment

#### Preconditions
- `validators` table populated (from 1.2)
- Consensus endpoint reachable

#### Steps
1. Collect all known validator public keys from replayed `validator_events` (both active and inactive)
2. Query Beacon API (`ValidatorsProvider.Validators`) at state `"head"` for all collected public keys
3. For each known validator: build a `models.Validator` with `public_key`, `active`, and beacon fields (`index`, `beacon_status`, `beacon_effective_balance`, activation/exit epochs, `beacon_slashed`, `beacon_withdrawable_epoch`)
4. Upsert each validator using `boil.Blacklist("migration_day")` on the update columns — this prevents beacon enrichment from overwriting `migration_day` values set by 1.3 or 1.4

#### Postconditions
- `validators` table enriched with beacon metadata (index, status, effective balance, lifecycle epochs)
- `migration_day` values preserved intact

---

### 1.6 Performance Sync

#### Preconditions
- `validators` table populated and enriched (from 1.2 + 1.5)
- Beaconcha.in endpoint reachable
- SSV API endpoint reachable

#### Steps
1. Determine `fromDay`: max of (first `ValidatorAdded` event's block time truncated to UTC day, plan's first round's first day)
2. Determine `toDay`: min of (`highestBlock` day - 1 day, `now - 2 days`, plan's last round's last day)
3. Error if `toDay < fromDay`
4. Update `state.earliest_validator_performance = fromDay`
5. Get all validators with beacon `index` set (known to the Beacon chain)
6. For each day in `[fromDay, toDay]`:
   a. Compute epoch range: `fromEpoch = spec.EpochAt(spec.SlotAt(beaconDay))`, `toEpoch = spec.EpochAt(spec.SlotAt(beaconDay + 1 day)) - 1`
   b. Sanity check: epoch range must span exactly one day
   c. Replay `validator_events` up to this day's last slot to determine active validators (track adds/removes; ignore liquidations, reactivations, and `ClusterMigratedToETH` events for active set)
   d. Skip if a `validator_performances` row already exists for this day and provider (with matching epoch range)
   e. Fetch SSV decideds: check memory cache, then file cache (with 48h freshness guard), then fetch from SSV API endpoint (`/validators/duty_counts/{fromEpoch}/{toEpoch}`); save to both caches
   f. For each active validator (concurrently, max 4 goroutines):
      - Set `solvent_whole_day = (activeSinceEpoch < fromEpoch)`
      - If validator has a beacon index: derive beacon status at `fromEpoch` and `toEpoch`, fetch performance from provider (`ValidatorPerformance`), populate attestation/proposal/sync committee stats and `end_effective_balance`
   g. Attach decideds count from SSV data to each performance row
   h. Bulk insert all performance rows in batches of 100
   i. Update `state.latest_validator_performance = day` within the same transaction
   j. Commit transaction

#### Postconditions
- `validator_performances` covers all active validators for all finalized days in the range
- 48h freshness guard on all file caches (SSV decideds and Beaconcha.in) ensures data stability
- `state.latest_validator_performance` = last processed day

---

## 2. Calculation Pipeline

### 2.1 Plan Loading & Validation

#### Preconditions
- `rewards.yaml` exists and is readable
- `rewards.sql` exists and is readable

#### Steps
1. Read and execute `rewards.sql` to create or replace stored procedures (`participations_by_validator`, `participations_by_owner`, `participations_by_recipient`, `exclusions_by_validator`)
2. Parse `rewards.yaml` into `Plan` struct via `rewards.ParsePlan`
3. Set default `LegacyCalculationCutoff` to `2025-08` if not specified
4. Validate mechanics: sorted by `since`, each has non-empty sorted tiers with positive `max_effective_balance` and non-negative `apr_boost`, valid criteria
5. Validate redirects: no conflict between inline and CSV for same type (owner/validator); load CSV files if specified
6. Validate `staking_upgrade` if present: `block` must be positive, `log_index` must be non-negative
7. Validate rounds: sorted, no duplicates, non-negative `network_fee`, positive `inflation_cap` (if specified)

#### Postconditions
- Plan is valid and ready for calculation
- SQL functions updated in DB
- Redirect CSVs loaded into plan structs

---

### 2.2 Round Completeness Determination

#### Preconditions
- Plan validated (from 2.1)
- `state.latest_validator_performance` and `state.earliest_validator_performance` set

#### Steps
1. Verify `earliest_validator_performance <= first round's first day` (hard error otherwise)
2. For each round: check `eth_apr > 0` AND `ssv_eth > 0`
3. Check `round.Period.LastDay() < state.latest_validator_performance + 1 day`
4. Collect rounds that pass all checks into `completeRounds`
5. Error if no complete rounds found

#### Postconditions
- List of complete rounds ready for processing
- Rounds with zero `eth_apr` or `ssv_eth` are silently skipped
- Default `network_fee` set to zero for rounds where it is nil

---

### 2.3 Redirect Setup (per round)

#### Preconditions
- Mechanics for the round determined (by `since` period)

#### Steps
1. If owner redirects exist: truncate `owner_redirects` table, verify empty, insert all redirects in a transaction, verify count matches
2. If validator redirects exist: truncate `validator_redirects` table, verify empty, insert all redirects in a transaction, verify count matches
3. Return boolean flags (`ownerRedirectsSupport`, `validatorRedirectsSupport`) for SQL function parameters

#### Postconditions
- `owner_redirects` and `validator_redirects` tables populated for this round
- Flags indicate whether SQL should apply redirects

---

### 2.4 Legacy Round Processing (pre-cutoff)

#### Preconditions
- `round.Period.Before(legacyCalculationCutoff)` (default cutoff: `2025-08`)
- Round is complete

#### Steps
1. Query `participations_by_validator` with `migration_filter='ssv'`
2. Query `participations_by_owner` with `migration_filter='ssv'`
3. Query `participations_by_recipient` with `migration_filter='ssv'`
4. Compute total EB (legacy method): sum of `TotalActiveEffectiveBalance / ActiveDays` per validator
5. Select tier based on total EB via `plan.Tier(period, totalEB)`
6. Compute daily reward (legacy): `annual / 12 / daysInMonth` where `annual = 32 ETH * eth_apr / ssv_eth * apr_boost`
7. For each validator, owner, and recipient row: compute reward and fee using `calculateReward` on the aggregated row
8. If `inflation_cap` configured and total rewards exceed cap: scale all rewards proportionally (`reward = reward * cap / totalRewards`)

#### State Output
- Single `roundResults` (SSV only, `ethResults` is always nil)

#### Postconditions
- Rewards computed preserving historical merkle-compatible semantics
- Fee calculation on aggregated rows (known multi-validator recipient bug preserved intentionally)

---

### 2.5 Modern Round Processing (post-cutoff, no migration)

#### Preconditions
- `round.Period >= legacyCalculationCutoff`
- `hasMigrationSupport` is false (`staking_upgrade` not configured)

#### Steps
1. Query `participations_by_validator` with `migration_filter='ssv'`
2. Compute total EB: if `period < tierCalculationCutoff (2025-11)`, use legacy method; otherwise use modern method (`Sum(TotalActiveEB) / periodDays`)
3. Select tier
4. Compute daily reward (modern): `annual / 365` where `annual = 32 ETH * eth_apr / ssv_eth * apr_boost`
5. **First pass**: compute base reward per validator (reward + fee) to check inflation cap; accumulate `totalBaseReward` and `originalRewardsWei`
6. If `inflation_cap` configured and `totalBaseReward > cap`: scale daily reward: `scaledDaily = daily * cap / totalBaseReward`
7. **Second pass**: compute final reward per validator with (potentially scaled) daily rate using `calculateReward`
8. Aggregate to owner level: group by `(ownerAddress, recipientAddress)`, sum days/EB/rewards/fees
9. Aggregate to recipient level: group by `recipientAddress`, sum days/EB/rewards/fees

#### State Output
- Single `roundResults` (SSV only)

#### Postconditions
- Per-validator fee computation ensures correctness for multi-validator recipients
- Inflation cap applied at the daily-rate level (not post-hoc proportional scaling)

---

### 2.6 Migration Round Processing (post-cutoff, with staking_upgrade)

#### Preconditions
- `round.Period >= legacyCalculationCutoff`
- `hasMigrationSupport` is true (`staking_upgrade` configured in plan)
- Some validators may have `migration_day` set

#### Steps
1. Query `participations_by_validator` with `migration_filter='ssv'` -> `ssvVPs`
2. Query `participations_by_validator` with `migration_filter='eth'` -> `ethVPs`
3. Combine `ssvVPs + ethVPs` into `allVPs` for tier selection
4. Compute combined total EB across both sets (using period-appropriate method)
5. Select tier based on combined EB -> single `apr_boost`
6. Compute single `dailyReward` from the shared tier
7. **First pass**: compute base reward for each SSV validator (with `ssvNetworkFee = round.NetworkFee`) and each ETH validator (with `ethNetworkFee = 0`); accumulate combined `totalBaseReward`
8. If `inflation_cap` configured and `totalBaseReward > cap`: scale `dailyReward`
9. **Second pass (SSV)**: compute final rewards with `ssvNetworkFee` using (potentially scaled) daily rate
10. **Second pass (ETH)**: compute final rewards with `ethNetworkFee = 0` using (potentially scaled) daily rate
11. Aggregate SSV results to owner/recipient levels in application code
12. Aggregate ETH results to owner/recipient levels in application code

#### State Output
- Two `roundResults`: `ssvResults` (SSV tree) and `ethResults` (ETH tree)

#### Postconditions
- Both trees share the same tier and daily reward rate
- ETH tree has zero network fees (no fee deductions)
- Combined inflation cap is applied across both trees simultaneously
- SSV + ETH active days for a migrated validator sum to total active days without migration

---

### 2.7 Network Fee Accumulation

#### Preconditions
- Round processing complete (SSV tree only — from 2.4, 2.5, or the SSV portion of 2.6)
- `network_fee_address` configured in mechanics (non-zero `ExecutionAddress`)

#### Steps
1. Sum all `feeDeduction` values across SSV recipient participations
2. If total fees > 0:
   a. Create synthetic owner entry for `network_fee_address` with total fees as reward, zero validators, and zero EB
   b. Create synthetic recipient entry for `network_fee_address` with same values
   c. Append to per-round and cumulative owner/recipient collections

#### Postconditions
- `network_fee_address` appears in `cumulative.json` (SSV tree) if any fees were collected
- `network_fee_address` does NOT appear in `cumulative-eth.json`
- Fee redistribution is zero-sum: sum of all fee deductions equals the network fee address reward

---

## 3. Export Pipeline

### 3.1 Per-Round Outputs

#### Steps
1. Write `by-validator.csv`, `by-owner.csv`, `by-recipient.csv` (tab-separated via gocsv) to `rewards/<network>/<period>/`
2. Build `cumulative.json`: running total of all recipient rewards (key: `0x<address>`, value: reward in Wei as string) across all rounds processed so far
3. If `ethResults` is non-empty for this round:
   a. Write `by-validator-eth.csv`, `by-owner-eth.csv`, `by-recipient-eth.csv`
   b. Build `cumulative-eth.json` (separate running total for ETH tree)

---

### 3.2 Cross-Round Totals

#### Steps
1. After all rounds: write `by-validator.csv`, `by-owner.csv`, `by-recipient.csv` to `rewards/<network>/` (all rounds combined)
2. Write `total-by-validator.csv`, `total-by-owner.csv`, `total-by-recipient.csv` (deduplicated totals per entity)
3. If any ETH results exist across all rounds: write all above with `-eth` suffix (`by-validator-eth.csv`, `total-by-validator-eth.csv`, etc.)

---

### 3.3 Exclusion Export

#### Steps
1. For each complete round: query `exclusions_by_validator` with `migration_filter='ssv'`
   - Exclusion reasons: `not_registered_whole_day`, `not_enough_attestations`, `not_enough_decideds`
2. Write `exclusions.csv`
3. If `staking_upgrade` configured: query `exclusions_by_validator` with `migration_filter='eth'`; write `exclusions-eth.csv` if results are non-empty

---

### 3.4 Inputs Archive

#### Steps
1. Copy `rewards.yaml` -> `inputs/rewards.yaml`
2. Marshal plan to JSON -> `inputs/rewards.json`
3. For each mechanics period with redirect files: export redirects to `inputs/<basename>.csv` (basename uniqueness across all redirect files is assumed — files with colliding basenames will overwrite each other)

---

## 4. Merkleization

### 4.1 SSV Tree
1. Sort `cumulative.json` keys: `jq -S . cumulative.json > scripts/merkle-generator/scripts/input_1.json`
2. From `scripts/merkle-generator/`: `npx hardhat run scripts/merkle.ts`
3. Output: root + per-leaf proofs in `scripts/merkle-generator/output_1.json`

### 4.2 ETH Tree
1. Sort `cumulative-eth.json` keys: `jq -S . cumulative-eth.json > scripts/merkle-generator/scripts/input_1.json`
2. From `scripts/merkle-generator/`: run the same Hardhat script
3. Output: separate root + per-leaf proofs in `scripts/merkle-generator/output_1.json`

---

## 5. Invariants

### 5.1 Existing

- **Event reconciliation**: Reconstructed active validator set from replayed `validator_events` (activated adds, deactivated removes) must exactly match SSV `nodeStorage.Shares()` non-liquidated set
- **Finalization-only**: Sync never processes blocks beyond the finalized block; `--to-block` above finalized is a hard error
- **48h freshness**: All file-cached external data (SSV decideds and Beaconcha.in performance) is only used if the cache timestamp is > 48h after the day's start
- **Idempotent schema**: `UNIQUE (block_number, log_index)` on `contract_events` and `PRIMARY KEY (provider, day, public_key)` on `validator_performances` prevent duplicate inserts
- **Legacy preservation**: Pre-cutoff rounds preserve historical merkle roots by using SQL-aggregated fee computation and month-length-dependent daily rewards
- **Epoch range sanity**: Each day's epoch range must span exactly `24h / (slotDuration * slotsPerEpoch)` epochs
- **State atomicity**: Contract events and `state.highest_block_number` are updated in the same transaction per block; performance rows and `state.latest_validator_performance` are updated in the same transaction per day

### 5.2 Migration-Specific

- **Single-tree-per-day**: A validator belongs to exactly one tree per day — SSV if `migration_day IS NULL OR day < migration_day`; ETH if `migration_day IS NOT NULL AND day >= migration_day` (enforced in SQL `CASE migration_filter`)
- **Set-once migration_day**: `migration_day` is set at most once per validator — all `UPDATE` statements include `WHERE migration_day IS NULL` guard
- **SSV tree includes network fee; ETH tree does not**: SSV uses `round.NetworkFee`; ETH uses `big.NewInt(0)` as network fee
- **Both trees share the same tier**: Combined EB from `ssvVPs + ethVPs` selects a single tier; both trees use the same `dailyReward`
- **Shared inflation cap**: If `inflation_cap` is configured, `totalBaseReward` spans both SSV and ETH validators; the scaled `dailyReward` is applied to both
- **Dedup within sync run**: Duplicate `ClusterMigratedToETH` events for the same cluster within a single sync run are silently skipped via in-memory `migratedClusters` map
- **Beacon enrichment safety**: `boil.Blacklist("migration_day")` on the upsert in 1.5 prevents beacon enrichment from overwriting the value
- **Day conservation**: For a validator with `migration_day` within a round: SSV `active_days` + ETH `active_days` = total active days the validator would have without migration (enforced by the complementary SQL `CASE` conditions)

---

## 6. Reproducibility

### 6.1 Verifier Procedure
1. Checkout the same commit/tag as the producer
2. Build with the repo's Dockerfile (toolchain pinned in `go.mod`)
3. Place producer's inputs: `rewards.yaml`, redirect CSVs
4. Run `sync --fresh --keep-cache`
5. Run `calc`
6. Compare `cumulative.json` and `cumulative-eth.json` (deterministic — JSON keys are sorted; `cumulative-eth.json` only exists when rounds have ETH participations). CSV row order may differ between runs (SQL queries have no ORDER BY, and Go map iteration in aggregation/totals is non-deterministic); compare reward amounts rather than raw SHA256 of CSVs
7. Sort and merkleize both cumulative JSONs (`jq -S .`)
8. Confirm SSV root and ETH root match the producer's published values
