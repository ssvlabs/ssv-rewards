AGENTS.md — Safety‑First Guide for ssv-rewards

Purpose and scope
- ssv-rewards synchronizes SSV validator activity and performance, then calculates monthly SSV-token rewards for the Incentivized Mainnet Program. Outputs feed Merkle roots published on Ethereum mainnet, enabling recipients to withdraw — there is no rollback once on-chain. Therefore, correctness and reproducibility are the top priorities; simplicity is the secondary goal.

Core responsibilities
- Fetch finalized SSV contract events and derive validator lifecycle (added/removed/liquidated/reactivated).
- Fetch per-validator daily performance (attestations/proposals/sync duties, effective balance, SSV “decideds”).
- Aggregate by validator/owner/recipient, apply reward mechanics and per-round parameters, and export CSV + cumulative JSON.
- Optionally merkleize a single round’s cumulative JSON into a Merkle tree for on-chain distribution.

High‑level architecture
- CLI entry: `cmd/ssv-rewards` exposes two subcommands:
  - `sync`: end-to-end data ingestion and state building. See `cmd/ssv-rewards/sync.go:1`.
  - `calc`: reward calculation and exports. See `cmd/ssv-rewards/calc.go:1`.
- Data plane and models:
  - PostgreSQL schema and stored procedures: `schema.sql:1`, `rewards.sql:1`.
  - ORM models (SQLBoiler): `pkg/models/*` (generated; reflects schema).
- Sync pipeline:
  - Contract logs (execution layer): `pkg/sync/contract_events.go:1`.
  - Validator events (SSV event handler + reconciliation): `pkg/sync/validator_events.go:1`.
  - Performance provider (consensus + external API): Beaconcha.in only (`pkg/sync/performance/beaconcha/beaconcha.go:1`).
  - HTTP retry + backoff: `pkg/sync/httpretry/client.go:1`.
- Reward logic:
  - Plan and mechanics: `pkg/rewards/plan.go:1`, `pkg/rewards/mechanics.go:1`, `pkg/rewards/mechanics_encoding.go:1`, `pkg/rewards/period.go:1`.
  - Fixed‑precision math helpers: `pkg/precise/eth.go:1`.
- Beacon spec helpers: `pkg/beacon/spec.go:1`.
- Merkleization helper scripts (optional): `scripts/merkle-generator/*`.

Key data sources
- Execution client JSON-RPC (ETH L1) at `EXECUTION_ENDPOINT` — finalized logs for SSV registry/events.
- Consensus node Beacon API at `CONSENSUS_ENDPOINT` — genesis, slot/epoch mapping, validator indices/state.
- SSV API (`SSV_API_ENDPOINT`) — validator duty counts (“decideds”) per day/epoch range.
- Performance provider: Beaconcha.in HTTP API only (`BEACONCHA_ENDPOINT`, optional `BEACONCHA_API_KEY`).

Data flow (end‑to‑end)
1) Contract logs (execution → DB)
   - `SyncContractEvents` pulls logs from `fromBlock` to a finalized `toBlock` and stores raw logs + parsed metadata, including block time and derived beacon slot (`pkg/sync/contract_events.go:16`, `pkg/sync/contract_events.go:68`).
   - Parsing uses SSV’s ABI/event parser; unknown events are stored but ignored (`pkg/sync/contract_events.go:112`).

2) Validator lifecycle (events → validators table)
   - `SyncValidatorEvents` replays stored logs through SSV’s `EventHandler` to reconstruct validator add/remove/liquidate/reactivate events and writes `validator_events` and the `validators` table (`pkg/sync/validator_events.go:1`).
   - Safety reconciliation: it reconstructs the active validator set from recorded events and compares it to the handler’s view; mismatch is a hard error (`pkg/sync/validator_events.go:188`).
   - For known validators, it enriches with Beacon chain index and status (`pkg/sync/validator_events.go:214`).

3) Daily performance (validators × days)
   - Input span is clipped to: [first active day..min(highest finalized block day − 1, now − 2 days)], ensuring data is finalized and APIs have stable values (`pkg/sync/validator_performance.go:46`, `pkg/sync/validator_performance.go:71`).
   - For each day, compose epoch range from beacon spec and validator activation/exit (`pkg/beacon/spec.go:9`, `pkg/sync/performance/beaconcha/beaconcha.go:160`).
   - Fetch “decideds” (SSV duties) with aggressive retry/backoff and write-through caches to disk and memory (`pkg/sync/validator_performance.go:126`, `pkg/sync/httpretry/client.go:1`).
   - Fetch performance from Beaconcha.in: per-index daily stats with on-disk and in‑memory caches; only use cached entries fetched ≥48h after the day (freshness guard) (`pkg/sync/performance/beaconcha/beaconcha.go:44`, `pkg/sync/performance/beaconcha/beaconcha.go:77`).
   - Bulk insert batched rows into `validator_performances` and update `state.latest_validator_performance` atomically (`pkg/sync/validator_performance.go:332`).

4) Calculation and exports
   - Stored procedures are (re)applied before calculation: `rewards.sql` (`cmd/ssv-rewards/calc.go:28`).
   - The plan is parsed from `rewards.yaml` into a validated `Plan` (`cmd/ssv-rewards/calc.go:40`, `pkg/rewards/plan.go:20`).
   - Guardrails: requires contiguous performance coverage from plan’s first round up to at least the last complete round (`cmd/ssv-rewards/calc.go:96`).
   - “Complete round” = has non‑zero `eth_apr` and `ssv_eth` and its last day ≤ `state.latest_validator_performance + 1d` (`cmd/ssv-rewards/calc.go:112`).
   - Participation aggregation (SQL): per‑validator/owner/recipient functions with optional redirects and Pectra support (`rewards.sql:1`, `rewards.sql:60`, `rewards.sql:101`).
   - Fee handling and rewards per round:
     - Pre‑cutoff (legacy) rounds compute fees on aggregated rows to preserve historical Merkle roots (`pkg/rewards/plan.go:32`, `cmd/ssv-rewards/calc.go:492`).
     - Post‑cutoff rounds compute fees per validator before aggregation (correct for multi‑validator recipients) (`cmd/ssv-rewards/calc.go:568`).
   - Reward formula (see `cmd/ssv-rewards/calc.go:740`):
     - unitBase = 32e9 Gwei × roundDays.
     - baseRewardᵢ = (dailyReward × roundDays × ΣwActiveEBᵢ) / unitBase.
     - rawFeeᵢ = max((networkFee × ΣwRegEBᵢ)/unitBase − (networkFee × registeredDaysᵢ)/roundDays, 0).
     - finalFeeᵢ = min(baseRewardᵢ, rawFeeᵢ); finalRewardᵢ = baseRewardᵢ − finalFeeᵢ.
- Exports (tab‑separated): per round and totals by validator/owner/recipient + cumulative JSON per round (`cmd/ssv-rewards/calc.go:375`, `cmd/ssv-rewards/calc.go:390`).
   - Optional network‑fee recipient entry is appended if `network_fee_address` is configured in mechanics (`cmd/ssv-rewards/calc.go:289`).

Configuration and environment
- `.env` (see `.env.example:1`):
  - `NETWORK` (e.g., `mainnet`), `CONSENSUS_ENDPOINT`, `EXECUTION_ENDPOINT`, `SSV_API_ENDPOINT`.
  - Beaconcha.in only: set `BEACONCHA_ENDPOINT` (+ optional `BEACONCHA_API_KEY`, `BEACONCHA_REQUESTS_PER_MINUTE`). Leave `E2M_ENDPOINT` empty.
  - `KEEP_CACHE` controls preserving `data/<network>/.cache` during fresh syncs.
- Rewards plan (`rewards.yaml`): must match the v2 schema used by `pkg/rewards` and `rewards.sql`:
  - mechanics: tiers by `max_effective_balance` and `apr_boost`, activity criteria, optional redirects, `pectra_support`, `network_fee_address` (`pkg/rewards/mechanics.go:15`, `pkg/rewards/mechanics.go:41`).
  - rounds: list of `{period, eth_apr, ssv_eth, network_fee?}` (`pkg/rewards/plan.go:214`).
  - Optional `legacy_calculation_cutoff` (default 2025‑08) to pin historical behavior (`pkg/rewards/plan.go:51`).
  - Known pitfall: an older experimental schema with `max_participants` exists in this repo at `rewards.yaml:1` which does NOT match the current code. Use `rewards.example.yaml:1` as your template and copy to `rewards.yaml`.

Database schema (safety‑relevant tables)
- `state`: singleton row guards the synced block window and performance coverage (`schema.sql:1`).
- `contract_events`: raw logs + derived metadata (`schema.sql:9`).
- `validators`: current beacon metadata, active flag maintained by event replay (`schema.sql:24`).
- `validator_events`: normalized validator lifecycle events with owner and public key (`schema.sql:37`).
- `validator_performances`: daily provider stats, SSV decideds, and end_effective_balance (`schema.sql:65`).
- Redirect tables: `owner_redirects` and `validator_redirects` (`schema.sql:54`, `schema.sql:59`).
- Aggregation/explanation functions: `participations_by_*`, `exclusions_by_validator` (`rewards.sql:1`, `rewards.sql:144`).

Safety and integrity guarantees (and where they live)
- Finalization only: upper bound defaults to the highest finalized block; non‑final blocks are rejected (`cmd/ssv-rewards/sync.go:191`).
- Fresh syncs reset schema and/or SSV tables deterministically; optional cache preservation avoids API churn while guaranteeing DB cleanliness (`cmd/ssv-rewards/sync.go:41`, `cmd/ssv-rewards/sync.go:65`).
- Event set reconciliation: reconstructed validator set from `validator_events` must match SSV handler’s set; any drift halts the run (`pkg/sync/validator_events.go:188`).
- 48‑hour freshness guard for performance: prevents using incomplete or unstable API data (`pkg/sync/performance/beaconcha/beaconcha.go:71`, `pkg/sync/validator_performance.go:71`).
- Provider staleness checks: e2m head‑slot must be current enough for the requested epochs (`pkg/sync/performance/e2m/e2m.go:59`).
- Pectra compatibility flag: when enabled, use per‑day end effective balance instead of fixed 32 ETH in SQL aggregations (`rewards.sql:27`, `pkg/rewards/mechanics.go:45`).
- Two‑phase rewards behavior:
  - Legacy (pre‑cutoff): preserve already published merkle roots by maintaining historical bugs/semantics (`pkg/rewards/plan.go:142`, `cmd/ssv-rewards/calc.go:492`).
  - Modern (post‑cutoff): correct fee handling by computing per‑validator fees before aggregation (`cmd/ssv-rewards/calc.go:568`).
- Defensive SQL constraints: primary keys and unique indexes ensure idempotence and deduplication (`schema.sql:19`, `schema.sql:44`, `schema.sql:106`).

Known risks and how to avoid them
- Mismatched rewards.yaml schema: The file `rewards.yaml:1` in this repo is an older, incompatible draft. Replace it with a copy of `rewards.example.yaml:1` and then edit. If the plan fails to parse, the app exits early (`cmd/ssv-rewards/main.go:39`).
- Incomplete rounds: If you set `eth_apr` or `ssv_eth` to zero or forget values, rounds are skipped as incomplete (`cmd/ssv-rewards/calc.go:112`).
- Redirect conflicts: You cannot provide both inline redirects and CSV files for the same redirect type (owner/validator); validation fails (`pkg/rewards/plan.go:94`).
- Using non‑final blocks: Passing `--to-block` above finalized halts sync (`cmd/ssv-rewards/sync.go:120`).
- Over‑freshening: `--fresh` drops schema and wipes data; use `--keep-cache` if you want to preserve `.cache` API results to reduce external load (`README.md:146`).
- Provider selection: Use Beaconcha.in; do not set `E2M_ENDPOINT` (`cmd/ssv-rewards/sync.go:236`).
- Performance coverage: The calc step enforces that coverage includes the first round’s full month; otherwise it fails (`cmd/ssv-rewards/calc.go:102`).
- Fee semantics across cutoff: Do not change `legacy_calculation_cutoff` unless you intend to re‑publish historical merkle trees with different roots.

Operational workflow (happy path, Beaconcha‑only)
- One‑time setup
  - `cp .env.example .env` and set Beaconcha.in endpoint/API key and rate limit (`README.md:9`).
  - `cp rewards.example.yaml rewards.yaml` and fill in mechanics/rounds (`README.md:23`).
  - Start Postgres: `docker compose up -d postgres` (`README.md:118`).
- Sync (repeatable)
  - `docker compose run --rm sync` — waits for finalization, reconstructs validator set, fetches daily performance with caching; expect long initial run (`README.md:126`).
  - Tip: To re‑sync cleanly but keep API caches: `docker compose run --rm sync sync --fresh --keep-cache` (`README.md:140`).
- Calculate
  - `docker compose run --rm calc` — produces per‑round and total CSVs and `rewards/<period>/cumulative.json` (`README.md:158`).
- Merkleization (one round at a time)
  - Copy `rewards/<period>/cumulative.json` → `scripts/merkle-generator/scripts/input_1.json` and run the script; outputs root + proofs in `output_1.json` (`README.md:172`).
  - To ensure a deterministic leaf order across machines, sort the cumulative map by address first, e.g.:
    - `jq -S . rewards/<period>/cumulative.json > scripts/merkle-generator/scripts/input_1.json`
    - Then run Hardhat. This avoids leaf-order–dependent root drift when multiple users reproduce the run.

Pre‑flight checklist for a production run
- Verify `.env` endpoints are reachable and synced; consensus and execution nodes must be healthy and finalized.
- Ensure `rewards.yaml` conforms to v2 schema; validate redirects (no duplicates/conflicts) and consider `pectra_support` if targeting Pectra semantics.
- Confirm `rounds` include the intended month(s) with non‑zero `eth_apr` and `ssv_eth` values. Network fees are specified in SSV tokens (18 decimals), not “gwei”.
- Use Beaconcha.in and set appropriate rate limits/keys; consider `--keep-cache` for reliability and speed.
- If publishing historical periods, confirm `legacy_calculation_cutoff` matches what produced on‑chain roots previously.

Validation and audit tips
- Re-run calc with the same DB snapshot and plan to verify deterministic outputs; CSVs and cumulative JSON should be byte‑identical.
- Inspect exclusions via `exclusions.csv` to understand why validators or days were excluded (`cmd/ssv-rewards/calc.go:1089`, `rewards.sql:144`).
- Spot‑check a validator’s day math by comparing `validator_performances` rows to provider responses and duty counts in the `.cache` directory (`pkg/sync/validator_performance.go:431`).
- For a round, recompute total effective balance from per‑validator active days and ensure it matches the logged `total_effective_balance` (`cmd/ssv-rewards/calc.go:422`).

Reproducibility recipe (verifiers)
- Use the same repo commit/tag as the producer; build with the provided Dockerfile (toolchain pinned in `go.mod`).
- Place the producer’s release inputs into repo root for the run:
  - `cp /path/to/release/inputs/rewards.yaml ./rewards.yaml`
  - `cp /path/to/release/inputs/owner-redirects-*.csv ./`
  - `cp /path/to/release/inputs/validator-redirects-*.csv ./` (as needed)
- Run `docker compose run --rm sync sync --fresh --keep-cache` and then `docker compose run --rm calc`.
- Compare SHA256 of `rewards/<period>/*` CSVs and `rewards/<period>/cumulative.json` with the producer’s bundle.
- Before merkleization, sort cumulative keys: `jq -S . rewards/<period>/cumulative.json > scripts/merkle-generator/scripts/input_1.json`, then run Hardhat; confirm the root equals producer’s.

Implementation notes (for contributors)
- Keep calculations in integers for Wei/Gwei; only format to decimals at the edges (`pkg/precise/eth.go:49`).
- SQL is the single source of truth for participation/aggregation; application code calls these functions and then computes rewards and fees (`cmd/ssv-rewards/calc.go:812`, `rewards.sql:1`).
- Do not widen the allowed data window beyond finalized blocks or the two‑day freshness guard without a documented rationale.
- When extending mechanics, update both YAML parsing/validation and SQL functions so behavior stays consistent.

Directory map (selected)
- CLI: `cmd/ssv-rewards`.
- Sync: `pkg/sync/*`.
- Rewards logic: `pkg/rewards/*`.
- Math: `pkg/precise/*`.
- Beacon helpers: `pkg/beacon/*`.
- Schema/SQL: `schema.sql`, `rewards.sql`.
- Outputs: `rewards/`.
- Merkleization: `scripts/merkle-generator/*`.

Mainnet production profile (as of September 2025)
- Mechanics/tiers and criteria are versioned by `since` period; current production inputs (see release artifact `inputs/`) reflect:
  - Redirects via external CSVs for owners since 2023‑07; validator redirects added in 2025‑03.
  - Activity thresholds increased to `min_attestations_per_day: 213` from 2025‑05 onwards; remained `22` for `min_decideds_per_day`.
  - Pectra support enabled since 2025‑06 (uses per‑day end effective balance).
  - Network fee address configured since 2025‑06.
  - Per‑round network_fee values present from 2025‑06 to 2025‑09 (units: SSV tokens, 18 decimals).
  - Some rounds may include `inflation_cap` in the YAML; this key is ignored by current code and is informational only.

Publishing and verification (current practice)
- Monthly (no code changes): a single designated producer runs the current released version and publishes the new month’s results. Others typically do not reproduce the run for that month.
- Before a new software release (code changes): a small set of users reproduce the results on the same commit to confirm determinism before approving the release.

Suggested verifier procedure (pre‑release; not an official guide)
- Checkout the same commit/tag as the producer; build using the repo’s Dockerfile.
- Use the producer’s release inputs (`inputs/` bundle for that round).
- Run `sync --fresh --keep-cache`, then `calc`.
- Compare SHA256 of CSVs and `cumulative.json` to the producer’s bundle.
- Sort cumulative keys (`jq -S`) prior to merkleization and confirm the Merkle root matches the producer’s.

Open questions (please confirm)
- Retain the root `rewards.yaml` as an example only (no change now), and keep the v2 plan used for production inside release artifacts under `inputs/`.
