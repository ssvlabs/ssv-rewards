# SSV Rewards — Technical Specification

This document is the **source of truth** for design intent, rules, and formulas of the ssv-rewards system. It covers the reward calculation pipeline, validator lifecycle, dual reward trees, and all invariants that guarantee correctness and reproducibility.

| Document | Purpose |
|---|---|
| **SPEC.md** (this file) | Design intent, rules, formulas, invariants, source of truth |
| **FLOWS.md** | Step-by-step execution, preconditions, state mutations, test checklist |

## Table of Contents

1. [Overview](#1-overview)
2. [Key Concepts](#2-key-concepts)
3. [Dual Reward Trees (SSV / ETH)](#3--dual-reward-trees-ssv--eth)
4. [Staking Upgrade Boundary](#4--staking-upgrade-boundary)
5. [Validator Lifecycle & Eligibility](#5-validator-lifecycle--eligibility)
6. [Reward Formula](#6-reward-formula)
7. [Effective Balance & Tier Selection](#7-effective-balance--tier-selection)
8. [Inflation Cap](#8-inflation-cap)
9. [Activity Criteria & Exclusions](#9-activity-criteria--exclusions)
10. [Redirects](#10-redirects)
11. [Legacy Calculation Behavior](#11-legacy-calculation-behavior)
12. [Data Sources & Finalization](#12-data-sources--finalization)
13. [Configuration Reference](#13-configuration-reference)
14. [Output Artifacts](#14-output-artifacts)
15. [Safety Invariants](#15-safety-invariants)
16. [References](#16-references)

---

## 1. Overview

ssv-rewards synchronizes SSV validator activity and performance data from Ethereum, then calculates monthly SSV-token rewards for the Incentivized Mainnet Program. Outputs feed Merkle roots published on Ethereum mainnet, enabling recipients to withdraw. There is no rollback once on-chain, so correctness and reproducibility are the top priorities.

The system exposes two CLI commands:

- **`sync`** — end-to-end data ingestion: fetches finalized contract events, reconstructs validator lifecycle, and collects daily performance data into PostgreSQL.
- **`calc`** — reward calculation: reads the synced database and a `rewards.yaml` plan, computes per-round rewards, and exports CSV files and cumulative JSON for merkleization.

The end-to-end flow is: contract logs are fetched from the execution layer and stored. Validator lifecycle events (add/remove/liquidate/reactivate/migrate) are replayed to build the active set. Daily performance is fetched from the SSV API (duty counts / decideds) and Beaconcha.in (attestations, sync committee, proposals, effective balance). The calc step then runs SQL aggregation functions, applies the reward formula per round, and exports results.

## 2. Key Concepts

**Round**
: A single month's reward period, identified by a `YYYY-MM` period string. Contains `eth_apr` (consensus layer ETH staking APR as a decimal fraction), `ssv_eth` (SSV/ETH price), optional `network_fee` (in SSV), and optional `inflation_cap` (in SSV tokens).

**Period**
: A `YYYY-MM` date representing a calendar month. Determines the day range (`FirstDay` to `LastDay`) and the number of days in the round (`Days()`).

**Mechanics**
: A versioned configuration block (keyed by `since` period) that defines tiers, activity criteria, redirects, Pectra support, and the network fee address. The mechanics active for a round are the latest whose `since <= round.period`.

**Tier**
: A threshold defined by `max_effective_balance` (in ETH) and `apr_boost`. The total effective balance of all participating validators determines which tier applies.

**Active Day**
: A validator-day where `attestations_executed >= min_attestations_per_day` AND `decideds >= min_decideds_per_day` AND `solvent_whole_day = true`.

**Effective Balance (EB)**
: The staking weight of a validator on a given day. Pre-Pectra: fixed at 32 ETH. Post-Pectra (`pectra_support: true`): actual `end_effective_balance` from the Beacon chain.

**Provider**
: The source of daily validator performance data. `beaconcha` (Beaconcha.in API) is the production-supported provider. The code also contains an `e2m` provider path, but it is not recommended for production use.

**migration_day**
: A per-validator date column in the `validators` table. `NULL` means the validator belongs to the SSV reward tree. A non-NULL value means the validator belongs to the ETH reward tree from that day onward.

**migration_filter**
: A SQL parameter (`'ssv'` or `'eth'`) passed to participation functions. Partitions validator-days into disjoint SSV and ETH sets based on `migration_day`.

## 3. Dual Reward Trees (SSV / ETH)

The core upgrade feature: when the staking upgrade is configured, the system produces two independent reward trees for each post-cutoff round (legacy rounds always remain SSV-only; see §11).

**Partitioning Rule.** The SQL `migration_filter` parameter controls which validator-days are included:

```
'ssv':  v.migration_day IS NULL  OR  vp.day < v.migration_day
'eth':  v.migration_day IS NOT NULL  AND  vp.day >= v.migration_day
```

This partitioning is exhaustive (every validator-day falls into exactly one tree) and non-overlapping.

**SSV Tree.** Includes network fee deduction. Produces `cumulative.json`. This is the traditional reward tree.

**ETH Tree.** Network fee is forced to zero (`ethNetworkFee = 0`), so `finalFee = 0` and `finalReward = baseReward`. Produces `cumulative-eth.json`.

Each tree is merkleized independently (two separate Hardhat runs).

**Backward Compatibility.** When no validators have `migration_day` set, the `'ssv'` filter matches all validator-days. The ETH tree is empty. The system behaves identically to the pre-upgrade version. This holds regardless of whether `staking_upgrade` is configured — if no validators have migrated, the ETH tree is simply empty.

## 4. Staking Upgrade Boundary

**Configuration.** The `staking_upgrade` block in `rewards.yaml`:

```yaml
staking_upgrade:
  block: 22345678
  log_index: 42
```

The boundary is defined as `(block_number, log_index)`. A log is considered post-upgrade if:

```
log.block_number > upgrade.block
  OR (log.block_number == upgrade.block AND log.log_index >= upgrade.log_index)
```

**ClusterMigratedToETH Event.** Emitted when an existing SSV cluster migrates to ETH payments on-chain. Processing:

1. The event is decoded to extract `owner`, `operatorIds`, and `cluster.validatorCount`.
2. The cluster ID is computed from `(owner, operatorIds)`.
3. All validators in the cluster are looked up via the SSV node storage.
4. Share count is validated against on-chain `validatorCount` (mismatch is a hard error).
5. `migration_day` is set on each validator to `block_time` truncated to calendar day (UTC).
6. An `IS NULL` guard on the update prevents overwriting an already-set `migration_day`.
7. A `validator_events` record with `event_name = "ClusterMigratedToETH"` is inserted per validator.

**Post-Upgrade ValidatorAdded.** Validators added at or after the upgrade boundary automatically receive `migration_day` set to the block time truncated to calendar day. They start earning in the ETH tree directly.

**When Not Configured.** If `staking_upgrade` is `nil` in the plan, `isPostStakingUpgrade` always returns `false` and no post-upgrade `ValidatorAdded` processing occurs. However, `handleClusterMigration` is always called when a `ClusterMigratedToETH` event topic is detected, regardless of configuration. On the calc side, `hasMigrationSupport` is false, so `processRound` (not `processRoundWithMigration`) is used — migration_day values are effectively ignored in reward calculation.

## 5. Validator Lifecycle & Eligibility

Validators transition through lifecycle events that control their reward eligibility:

| Event | Effect | `active` | `migration_day` |
|---|---|---|---|
| `ValidatorAdded` | Starts earning rewards | `true` | Set if post-upgrade |
| `ValidatorRemoved` | Stops earning | `false` | Unchanged |
| `ClusterLiquidated` | Stops earning | `false` | Unchanged |
| `ClusterReactivated` | Resumes earning | `true` | Unchanged |
| `ClusterMigratedToETH` | Switches to ETH tree | `true` (unchanged) | Set to block day |

A validator earns rewards on a day if it was added before the day's first epoch and not removed before the day ends (`solvent_whole_day = true`). Note: the per-day active set for performance sync only tracks `ValidatorAdded`/`ValidatorRemoved` events; `ClusterLiquidated` and `ClusterReactivated` are ignored because liquidated validators naturally fail activity criteria (decideds drop to zero when operators stop running them).

**Reconciliation Invariant.** After processing all events, the active validator set reconstructed from `validator_events` (replaying `activated` flags) must exactly match the set from SSV's `EventHandler`. Any mismatch is a hard error that halts the run.

## 6. Reward Formula

### 6.1 Annual Reward Rate

For a given round and tier:

```
annual = (BaseEffectiveBalance * eth_apr) / ssv_eth * apr_boost
```

Where `BaseEffectiveBalance = 32 ETH` (in 18-decimal precision).

### 6.2 Daily Reward Rate

Where `roundDays` is the number of days in the round's calendar month (`round.Period.Days()` in code).

**Legacy** (periods before `legacy_calculation_cutoff`, default `2025-08`):

```
monthly = annual / 12
daily   = monthly / roundDays
```

**Modern** (periods from `legacy_calculation_cutoff` onwards):

```
daily   = annual / 365
monthly = daily * roundDays
```

### 6.3 Base Reward

```
unitBase      = BaseEffectiveBalance_gwei * roundDays    // 32e9 * roundDays
rewardTier    = dailyReward * roundDays

baseReward_i  = (rewardTier * wActiveEB_i) / unitBase
```

Where `wActiveEB_i` is the sum of effective balance (in Gwei) across all active days for validator `i`.

This simplifies to: `dailyReward * wActiveEB_i / 32e9` (the `roundDays` cancel).

### 6.4 Network Fee Deduction (SSV tree only)

```
feeFromEB_i  = (networkFee * wRegEB_i) / unitBase
feeCredit_i  = (networkFee * registeredDays_i) / roundDays
rawFee_i     = max(feeFromEB_i - feeCredit_i, 0)
finalFee_i   = min(baseReward_i, rawFee_i)
```

Where `wRegEB_i` is the sum of effective balance across all registered days (active or not) and `registeredDays_i` is the count of registered days.

### 6.5 Final Reward

```
finalReward_i = baseReward_i - finalFee_i
```

### 6.6 Network Fee Recipient

If `network_fee_address` is configured in mechanics, the sum of all `finalFee` values across recipients is assigned to a synthetic entry for that address. This entry is appended to owner and recipient participations.

### 6.7 ETH Tree

For the ETH tree, `networkFee` is set to `0`:

```
rawFee_i    = 0
finalFee_i  = 0
finalReward_i = baseReward_i
```

No network fee recipient entry is generated for the ETH tree.

## 7. Effective Balance & Tier Selection

### 7.1 Pre-Pectra

When `pectra_support: false` (or omitted), every validator-day uses a fixed effective balance of `32000000000` Gwei (32 ETH).

### 7.2 Post-Pectra

When `pectra_support: true`, the actual `end_effective_balance` from `validator_performances` is used per day. This reflects Ethereum's Pectra upgrade allowing variable effective balances.

### 7.3 Total Effective Balance

**Legacy** (periods before `tierCalculationCutoff = 2025-11`):

```
totalEB = sum over validators of (TotalActiveEB_i / ActiveDays_i)
```

Each validator contributes its average daily effective balance.

**Modern** (periods from `2025-11` onwards):

```
totalEB = sum(TotalActiveEB across all validators) / periodDays
```

The total is the average daily effective balance across the entire round.

### 7.4 Tier Lookup

Tiers are sorted by `max_effective_balance` in ascending order. The first tier where `totalEB <= max_effective_balance` is selected. Its `apr_boost` is used in the reward rate calculation. If `totalEB` exceeds all tiers' `max_effective_balance`, calculation fails.

### 7.5 Combined Effective Balance

When migration is active (`processRoundWithMigration`), SSV and ETH validator participations are combined into a single list for effective balance calculation and tier selection. Both trees receive the same `tier`, `apr_boost`, and `dailyReward`.

## 8. Inflation Cap

### 8.1 Configuration

Optional per-round `inflation_cap` in SSV tokens (18-decimal precision). Must be positive if specified.

### 8.2 Legacy Scaling

For rounds before `legacy_calculation_cutoff`: if `totalRoundRewards > inflation_cap`, each participant's reward is proportionally scaled at all three aggregation levels (validator, owner, and recipient) independently:

```
scaled_reward_i = reward_i * inflation_cap / totalRoundRewards
```

### 8.3 Modern Scaling

For rounds from `legacy_calculation_cutoff` onwards: if combined base rewards exceed the cap, the daily reward rate itself is scaled down:

```
scaledDailyReward = dailyReward * inflation_cap / totalBaseReward
```

Then all rewards and fees are recomputed with the scaled rate.

### 8.4 Combined Cap

When migration is active, the inflation cap applies to the **combined** SSV + ETH base rewards:

```
totalBaseReward = sum(baseReward + fee) across all SSV validators
               + sum(baseReward + fee) across all ETH validators
```

If `totalBaseReward > inflation_cap`, the same `scaledDailyReward` is used for both trees. This ensures a single consistent scaling factor.

## 9. Activity Criteria & Exclusions

A validator-day is considered **active** when all three conditions are met:

1. `attestations_executed >= min_attestations_per_day`
2. `decideds >= min_decideds_per_day`
3. `solvent_whole_day = true` (the validator was registered for the entire day)

Only validators with at least one active day in the period are included in participations (`HAVING COUNT(*) FILTER (WHERE is_active) > 0`).

**Exclusion Reasons** (mutually exclusive, checked in order):

| Reason | Condition |
|---|---|
| `not_registered_whole_day` | `solvent_whole_day = false` |
| `not_enough_attestations` | `attestations_executed < min_attestations_per_day` |
| `not_enough_decideds` | `decideds < min_decideds_per_day` |

**Per-Tree Exclusions.** Exclusions are exported separately: `exclusions.csv` uses `migration_filter = 'ssv'` and `exclusions-eth.csv` uses `migration_filter = 'eth'`. ETH exclusions are only exported when `staking_upgrade` is configured and the result set is non-empty.

**Policy-Level Exclusions (not enforced by code).** DAO proposals define additional eligibility rules — slashed validators, US/OFAC/sanctioned participants, and reward address attribution exceptions (SAFE multisigs, Lido SimpleDVT/CSM, historical deployer mappings). These rules are not automatically enforced by the pipeline. They are handled operationally through redirect CSVs, manual exclusions, and review before publishing Merkle roots.

## 10. Redirects

Redirects allow rerouting rewards to a different recipient address.

**Owner Redirects.** Map `owner_address -> recipient_address`. All validators owned by the address receive rewards at the redirect target.

**Validator Redirects.** Map `public_key -> recipient_address`. Overrides owner redirects for specific validators.

**Precedence** (in SQL):

```sql
COALESCE(
    CASE WHEN validator_redirects_support THEN vr.to_address END,
    CASE WHEN owner_redirects_support THEN owr.to_address END,
    vp.owner_address
)
```

Validator redirect takes priority over owner redirect, which takes priority over the default owner address.

**Loading.** Redirects can be specified inline in `rewards.yaml` or via external CSV files (header: `from,to`). Both methods cannot be used simultaneously for the same redirect type within a mechanics block.

**Versioning.** Each mechanics block (keyed by `since`) has its own set of redirects, allowing redirects to change across periods.

## 11. Legacy Calculation Behavior

`legacy_calculation_cutoff` (default: `2025-08`) separates two calculation modes:

**Legacy (periods before cutoff):**
- Daily reward varies by month length: `annual / 12 / roundDays`.
- Fees are computed on SQL-aggregated rows (after grouping by recipient). This causes incorrect fee calculation for recipients with multiple validators, but preserves published Merkle roots.
- Inflation cap scaling is applied post-hoc by proportionally reducing final rewards.
- `processRoundLegacy` always passes `migration_filter = 'ssv'` and never produces ETH tree output (`ethResults` is always `nil`).

**Modern (periods from cutoff onwards):**
- Daily reward is constant: `annual / 365`.
- Fees are computed per validator before aggregation by owner/recipient (correct for multi-validator recipients).
- Inflation cap scales the daily reward rate before computing individual rewards.

## 12. Data Sources & Finalization

| Source | Data | Endpoint Variable |
|---|---|---|
| Execution client (JSON-RPC) | Finalized logs from SSV contract | `EXECUTION_ENDPOINT` |
| Consensus node (Beacon API) | Genesis, beacon spec (slot duration, slots per epoch), validator indices/status, effective balance | `CONSENSUS_ENDPOINT` |
| SSV API | Duty counts ("decideds") per epoch range | `SSV_API_ENDPOINT` |
| Beaconcha.in | Per-validator daily performance stats | `BEACONCHA_ENDPOINT` |

**Finalization-Only.** The upper block bound defaults to the highest finalized block on the execution client. Non-finalized blocks are rejected.

**48-Hour Freshness Guard.** File-cached data for a given day is only reused if the cache entry was created at least 48 hours after the day's start (midnight UTC). In-memory cache entries within the same process run are derived from validated file-cache reads or fresh API responses. See §15.2 for details.

**Performance Window.** The caller passes `[first_round_first_day, last_round_last_day]` from the plan. The sync function then clips this to `[max(from, earliest_active_day), min(to, highest_block_day - 1, now - 2 days)]`.

## 13. Configuration Reference

### rewards.yaml Schema

```yaml
version: 2                        # Informational only — not validated by the parser

# Optional: upgrade boundary for ETH-fee cluster migration.
staking_upgrade:              # Optional
  block: <int>                # Block number (must be positive)
  log_index: <int>            # Log index (must be non-negative)

# Optional: cutoff for legacy calculation methods.
legacy_calculation_cutoff: "2025-08"   # Default if omitted

mechanics:
  - since: "YYYY-MM"
    criteria:
      min_attestations_per_day: <int>
      min_decideds_per_day: <int>
    tiers:
      - max_effective_balance: <number>  # In ETH
        apr_boost: <number>
    # Redirects (inline OR file, not both per type)
    owner_redirects:
      "0x...": "0x..."
    owner_redirects_file: "path.csv"
    validator_redirects:
      "0x...": "0x..."
    validator_redirects_file: "path.csv"
    pectra_support: <bool>               # Default: false
    network_fee_address: "0x..."         # Optional

rounds:
  - period: "YYYY-MM"
    eth_apr: <number>
    ssv_eth: <number>
    network_fee: <number>                # Optional, in SSV tokens (18 decimals)
    inflation_cap: <number>              # Optional, must be positive if specified
```

**Validation Rules:**
- Mechanics must be sorted by `since` period.
- Tiers must be sorted by `max_effective_balance` (ascending), no duplicates.
- Rounds must be sorted by period, no duplicates.
- A round is complete only if `eth_apr > 0`, `ssv_eth > 0`, and its last day is covered by performance data.

## 14. Output Artifacts

### Per-Round Directory: `rewards/<network>/<period>/`

| File | Description |
|---|---|
| `by-validator.csv` | Per-validator participation and rewards (tab-separated) |
| `by-owner.csv` | Per-owner aggregated participation and rewards (grouped by `(owner, recipient)` — one owner may have multiple rows if redirects split across recipients) |
| `by-recipient.csv` | Per-recipient aggregated participation and rewards |
| `cumulative.json` | `{ "0xaddr": "reward_wei_string" }` for SSV tree merkleization |
| `by-validator-eth.csv` | ETH tree per-validator participation and rewards (only when round has ETH participations) |
| `by-owner-eth.csv` | ETH tree per-owner aggregated participation and rewards (only when round has ETH participations) |
| `by-recipient-eth.csv` | ETH tree per-recipient aggregated participation and rewards (only when round has ETH participations) |
| `cumulative-eth.json` | `{ "0xaddr": "reward_wei_string" }` for ETH tree merkleization (only when round has ETH participations) |

### Cross-Round Totals: `rewards/<network>/`

| File | Description |
|---|---|
| `by-validator.csv` | All rounds by validator |
| `by-owner.csv` | All rounds by owner |
| `by-recipient.csv` | All rounds by recipient |
| `total-by-validator.csv` | Aggregated totals by validator |
| `total-by-owner.csv` | Aggregated totals by owner (merged by `owner_address` only — multiple per-round recipient rows for the same owner are combined) |
| `total-by-recipient.csv` | Aggregated totals by recipient |
| All above with `-eth` suffix | ETH tree equivalents (only when ETH tree has entries) |

### Other Outputs

| File | Description |
|---|---|
| `exclusions.csv` | SSV tree excluded validator-days with reasons |
| `exclusions-eth.csv` | ETH tree excluded validator-days (only when `staking_upgrade` configured and result set is non-empty) |
| `inputs/rewards.yaml` | Copy of the plan used |
| `inputs/rewards.json` | JSON representation of the parsed plan |
| `inputs/*.csv` | Exported redirect data (regenerated from parsed plan, not byte-for-byte copies of originals). Files are written as `inputs/<basename>` — basename uniqueness across all redirect files is assumed |

### Merkleization

Merkleization is performed externally via a Hardhat script in `scripts/merkle-generator/` (`scripts/merkle.ts`). For deterministic leaf order across machines, sort cumulative keys before running:

```bash
jq -S . rewards/<network>/<period>/cumulative.json > scripts/merkle-generator/scripts/input_1.json
```

With dual trees, run merkleization twice: once for `cumulative.json` (SSV tree) and once for `cumulative-eth.json` (ETH tree).

## 15. Safety Invariants

### 15.1 Finalization-Only Blocks

The sync pipeline only processes blocks at or below the highest finalized block. Passing `--to-block` above the finalized head halts the sync.

### 15.2 48-Hour Freshness Guard

File-cached external data (both SSV decideds and Beaconcha.in performance) is only trusted if fetched at least 48 hours after the day's start (midnight UTC). In-memory cache entries are derived from validated file-cache reads or fresh API responses within the same process run. This prevents using incomplete or unstable API responses.

### 15.3 Event Set Reconciliation

After replaying all `validator_events`, the reconstructed active set must exactly match SSV's `EventHandler` view. Any mismatch (missing or extra validators) is a hard error.

### 15.4 Legacy Merkle Preservation

Rounds before `legacy_calculation_cutoff` use legacy semantics (month-varying daily rate, post-aggregation fee calculation) to produce identical reward amounts matching previously published Merkle roots. Note: `cumulative.json` is deterministic (JSON keys sorted), but CSV row order may vary between runs — SQL participation/exclusion queries have no `ORDER BY`, and Go map iteration in aggregation and total exports is also non-deterministic.

### 15.5 Idempotent Schema

Primary keys and unique indexes on `contract_events(block_number, log_index)`, `validators(public_key)`, `validator_events(block_number, log_index, owner_address, public_key)`, and `validator_performances(provider, day, public_key)` ensure idempotence and deduplication.

### 15.6 migration_day Set-Once

The `IS NULL` guard on the `UPDATE` statement prevents overwriting `migration_day` once set. This applies to both `handleClusterMigration` and post-upgrade `ValidatorAdded` processing:

```sql
WHERE public_key = $1 AND migration_day IS NULL
```

### 15.7 One Tree Per Validator-Day

The `migration_filter` SQL logic partitions all validator-days into exactly two disjoint sets:
- `'ssv'`: `migration_day IS NULL OR day < migration_day`
- `'eth'`: `migration_day IS NOT NULL AND day >= migration_day`

Every validator-day belongs to exactly one tree. No day is counted in both or excluded from both.

### 15.8 Duplicate ClusterMigratedToETH Silently Ignored

An in-memory `migratedClusters` map deduplicates `ClusterMigratedToETH` events within a single sync run. A duplicate logs a warning and returns without error.

### 15.9 Blacklist Prevents migration_day Overwrite

The beacon enrichment step in `SyncValidatorEvents` uses `boil.Blacklist("migration_day")` on the upsert, ensuring that updating beacon metadata (index, status, effective balance) never overwrites `migration_day` values set by cluster migration or post-upgrade add.

## 16. References

| Proposal | Description |
|---|---|
| [Original IMP](https://forum.ssv.network/t/incentivized-mainnet-program/1203) | Program inception, reward formula, tiered structure |
| [DIP-18](https://forum.ssv.network/t/dip-18-incentivized-mainnet-program-revision/1419) | Revision #1 — extension to Dec 2024, tier refactoring |
| [DIP-27](https://forum.ssv.network/t/dip-27-incentivized-mainnet-program-revision-2/1725) | Revision #2 — extension to Dec 2025, budget reset |
| [DIP-34](https://forum.ssv.network/t/dip-34-incentivized-mainnet-program-revision-3/1908) | Revision #3 — Pectra compatibility, 95% attestation threshold |
| [DIP-39](https://forum.ssv.network/t/dip-39-incentivized-mainnet-program-revision-4/2006) | Revision #4 — extension to Dec 2027, 15% inflation cap |
| [DIP-50](https://forum.ssv.network/t/dip-50-introduction-of-effective-balance-oracles-effective-balance-accounting-eth-denominated-payments-and-ssv-staking/2106) | Staking upgrade — dual Merkle trees, ETH payments, no NF on ETH clusters |
