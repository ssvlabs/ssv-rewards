# SSV Rewards — ETH Migration QA Plan

## Overview

Validators migrating to ETH-fee clusters receive a `migration_day` in the database. The reward calculation splits each validator's days into two independent trees:

- **SSV tree**: days before `migration_day` (with network fee deduction) — existing `cumulative.json`
- **ETH tree**: days from `migration_day` onward (no network fee) — new `cumulative-eth.json`

Each tree produces separate CSV and JSON outputs and is merkleized independently.

When no validators have migrated, all outputs are identical to the current release.

**Output locations:**
- Per-round files: `rewards/<period>/`
- Total files: `rewards/`

**Prerequisites:**
- New binary built from the PR branch
- Mainnet DB access (synced with previous release)
- `rewards.yaml` from the latest release inputs

> **Note:** All dates used in this document (`2025-09-01`, `2025-09-15`, etc.) are examples. Adjust to match the data available in your DB.

---

## Phase 1: Regression — No config changes

**Goal:** New code produces identical outputs when no migration config is present.

**Steps:**
1. Start from an existing mainnet DB synced with the previous release binary.
2. Run the schema migration:
   ```sql
   ALTER TABLE validators ADD COLUMN IF NOT EXISTS migration_day DATE;
   ```
3. Run `calc` with the new binary. Do **not** add `staking_upgrade` to `rewards.yaml`.
4. Generate merkle from `rewards/<latest-round>/cumulative.json` (see [Merkle generation](#merkle-generation)).

**Verify:**
- [ ] SSV merkle root matches the latest published round.
- [ ] No `-eth` output files are produced (`by-*-eth.csv`, `cumulative-eth.json`).

> **Important:** Save the `rewards/` output directory from this phase — later phases reference it for comparison.

---

## Phase 2: Sync modes

**Goal:** Verify different sync modes handle the `migration_day` column correctly.

### 2a: Fresh sync

1. Run `sync --fresh` (drops schema, recreates from scratch).
2. Verify `migration_day` column exists without manual ALTER:
   ```sql
   SELECT column_name FROM information_schema.columns
   WHERE table_name = 'validators' AND column_name = 'migration_day';
   -- Expected: 1 row
   ```
3. Run `calc`.
4. Verify: SSV merkle root matches Phase 1.

### 2b: Fresh sync with keep-cache

1. Run `sync --fresh --keep-cache`.
2. Verify `.cache` directory is preserved (files still present from previous sync).
3. Run `calc`.
4. Verify: SSV merkle root matches Phase 1.

### 2c: Incremental sync preserves migration_day

This validates the `boil.Blacklist` fix — incremental sync must not wipe `migration_day`.

1. Manually seed `migration_day` on a few validators:
   ```sql
   UPDATE validators SET migration_day = '2025-09-15'
   WHERE public_key IN (SELECT public_key FROM validators LIMIT 3);
   ```
2. Run `sync` (no `--fresh`).
3. Verify `migration_day` is preserved:
   ```sql
   SELECT count(*) FROM validators WHERE migration_day IS NOT NULL;
   -- Expected: 3
   ```
4. Clean up:
   ```sql
   UPDATE validators SET migration_day = NULL WHERE migration_day IS NOT NULL;
   ```

---

## Phase 3: Dual-tree — Comprehensive test

**Goal:** Verify the full dual-tree behavior with one carefully chosen seeding that covers all key scenarios.

### Setup

Pick a round that has both `network_fee` and `inflation_cap` configured (e.g., Sep 2025). The seeding should cover:

1. **Find a multi-validator owner** (for mixed-owner testing):
   ```sql
   SELECT owner_address, array_agg(DISTINCT vp.public_key) AS keys, count(DISTINCT vp.public_key) AS cnt
   FROM validator_performances vp
   WHERE vp.day >= '2025-09-01' AND vp.day < '2025-10-01' AND vp.solvent_whole_day
   GROUP BY owner_address
   HAVING count(DISTINCT vp.public_key) >= 4
   LIMIT 5;
   ```

2. **Migrate half of that owner's validators** mid-month:
   ```sql
   UPDATE validators SET migration_day = '2025-09-15'
   WHERE public_key IN ('...first_half...');
   ```

3. **Also migrate a few validators that have redirects** (skip if no redirects are configured in this environment):
   ```sql
   -- Find validators with redirects.
   SELECT v.public_key, vr.to_address
   FROM validators v
   JOIN validator_redirects vr ON v.public_key = vr.public_key
   LIMIT 5;

   -- Migrate some of them.
   UPDATE validators SET migration_day = '2025-09-15'
   WHERE public_key IN ('...redirected_keys...');
   ```

4. Run `calc`.
5. Generate merkles from both `cumulative.json` and `cumulative-eth.json` (see [Merkle generation](#merkle-generation)).

### Verify: Basic split

- [ ] ETH per-round files produced: `by-validator-eth.csv`, `by-owner-eth.csv`, `by-recipient-eth.csv`, `cumulative-eth.json`.
- [ ] ETH total files produced: `total-by-validator-eth.csv`, `total-by-owner-eth.csv`, `total-by-recipient-eth.csv`.
- [ ] In SSV CSVs: migrated validators show fewer `active_days` than Phase 1 (only pre-migration days).
- [ ] In ETH CSVs: migrated validators show `active_days` from `migration_day` onward only.
- [ ] SSV merkle root **differs** from Phase 1.
- [ ] ETH merkle tree is valid.

### Verify: Mixed owner

- [ ] SSV `by-owner.csv`: the multi-validator owner has all non-migrated validators' days + pre-migration days of migrated validators.
- [ ] ETH `by-owner-eth.csv`: same owner has only post-migration days of migrated validators.
- [ ] Non-migrated validators do **not** appear in ETH CSVs.
- [ ] Both `cumulative.json` and `cumulative-eth.json` contain this owner's recipient address.

### Verify: Redirects

- [ ] In SSV CSVs: redirected recipient receives pre-migration rewards.
- [ ] In ETH CSVs: same redirected recipient receives post-migration rewards.
- [ ] `cumulative-eth.json` uses the redirected address, not the original owner.

### Verify: Network fee

- [ ] In ETH CSVs: fee column is 0 for all rows.
- [ ] `cumulative.json` includes the `network_fee_address` with accumulated fees.
- [ ] `cumulative-eth.json` does **not** include the `network_fee_address`.

### Verify: Inflation cap

- [ ] If the round hit the cap in Phase 1: SSV total + ETH total for the round equals the Phase 1 total (cap is shared, total unchanged).
- [ ] If the round didn't hit the cap: SSV total + ETH total equals what the uncapped combined rewards would be.

### Verify: Tier selection

- [ ] Calc log output shows the tier is based on the **combined** effective balance (SSV + ETH), not per-tree.
- [ ] For a recipient appearing in both trees: the reward-per-active-day ratio is consistent between SSV and ETH CSVs (same tier was applied to both).

### Verify: Multi-round behavior

Ensure the migrated validators from setup have performance data in the month before and after the migration month (e.g., Aug and Oct if migrating in Sep). If not, pick additional validators that span 3 consecutive rounds.

- [ ] Round before the migration month: migrated validators appear in SSV only, not in ETH CSVs.
- [ ] Migration month: migrated validators appear in both trees (split by day).
- [ ] Round after the migration month: migrated validators appear in ETH only, not in SSV CSVs.

### Verify: Pectra effective balance

- [ ] SSV `by-validator.csv`: `total_active_effective_balance` reflects only pre-migration days' balances.
- [ ] ETH `by-validator-eth.csv`: `total_active_effective_balance` reflects only post-migration days' balances.

### Verify: Exclusions

- [ ] SSV `exclusions.csv`: no rows with `day >= migration_day` for migrated validators.
- [ ] ETH `exclusions-eth.csv`: no rows with `day < migration_day` for migrated validators.

---

## Phase 4: Edge cases

Reset before each test case:

```sql
UPDATE validators SET migration_day = NULL WHERE migration_day IS NOT NULL;
```

### 4a: First day of month

Set `migration_day` = first day of a round month, run `calc`.

- [ ] All days in that month go to ETH tree, none to SSV for that round.

### 4b: Last day of month

Set `migration_day` = last day of a round month, run `calc`.

- [ ] Only the last day goes to ETH tree for that round.

### 4c: Month with no round

Set `migration_day` in a month that has no configured round, run `calc`.

- [ ] No effect on any outputs.

### 4d: Legacy rounds

Set `migration_day` on validators to a date within a legacy round (before `legacy_calculation_cutoff`), run `calc`.

- [ ] Legacy rounds produce no `-eth` output files.
- [ ] Legacy round outputs are unchanged (compare against a clean run with no `migration_day` set).

---

## Phase 5: Sync event handling (future — when contract migrates on mainnet)

> This phase cannot be tested until `ClusterMigratedToETH` events are emitted on mainnet.

**Steps:**
1. Set `staking_upgrade` in `rewards.yaml` to the actual contract migration block and log_index.
2. Run `sync --fresh --keep-cache` against mainnet after the contract migration.
3. Run `calc`.

**Verify:**
- [ ] `validator_events` table contains `ClusterMigratedToETH` records for affected validators.
- [ ] `validators.migration_day` is set for all validators in migrated clusters.
- [ ] Re-running `sync` (without `--fresh`) does **not** overwrite `migration_day`.
- [ ] `ValidatorAdded` events **after** the `staking_upgrade` boundary → `migration_day` is set.
- [ ] `ValidatorAdded` events **before** the boundary → no `migration_day`.
- [ ] Dual-tree outputs produced correctly (repeat Phase 3 verifications).

---

## Cleanup

After all testing, reset the DB to clean state:

```sql
UPDATE validators SET migration_day = NULL WHERE migration_day IS NOT NULL;
```

---

## Merkle generation

```bash
jq -S . rewards/<period>/cumulative.json > scripts/merkle-generator/scripts/input_1.json
cd scripts/merkle-generator
npx hardhat run scripts/generate.js
# Root + proofs in scripts/output_1.json
```

Repeat with `cumulative-eth.json` for the ETH tree.

---

## SQL spot-checks

Useful queries for manual verification. Replace the criteria values (`213`, `22`) with the ones from the `mechanics` entry in `rewards.yaml` that covers the round you're testing.

**Compare SSV vs ETH participations for a migrated validator:**
```sql
SELECT * FROM participations_by_validator('beaconcha', 213, 22, '2025-09-01', NULL, true, true, true, 'ssv')
WHERE public_key = '...';

SELECT * FROM participations_by_validator('beaconcha', 213, 22, '2025-09-01', NULL, true, true, true, 'eth')
WHERE public_key = '...';
```

**Confirm total days are preserved (SSV + ETH = original):**

> This JOIN only returns validators that appear in **both** trees (i.e., `migration_day` falls within the round month). Validators fully in one tree won't appear.

```sql
SELECT
  ssv.active_days AS ssv_active_days,
  eth.active_days AS eth_active_days,
  ssv.active_days + eth.active_days AS total_active_days
FROM participations_by_validator('beaconcha', 213, 22, '2025-09-01', NULL, true, true, true, 'ssv') ssv
JOIN participations_by_validator('beaconcha', 213, 22, '2025-09-01', NULL, true, true, true, 'eth') eth
  USING (public_key)
WHERE ssv.public_key = '...';
```

**Verify mixed-owner split:**
```sql
SELECT * FROM participations_by_owner('beaconcha', 213, 22, '2025-09-01', NULL, true, true, true, 'ssv')
WHERE owner_address = '...';

SELECT * FROM participations_by_owner('beaconcha', 213, 22, '2025-09-01', NULL, true, true, true, 'eth')
WHERE owner_address = '...';
```
