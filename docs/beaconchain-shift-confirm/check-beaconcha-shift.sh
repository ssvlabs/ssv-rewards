#!/usr/bin/env bash
# check-beaconcha-shift.sh — confirm that beaconcha.in re-bucketed historical
# daily validator stats from beacon-chain days (genesis-aligned, 12:00:23 UTC)
# to UTC days (00:00:00 UTC), by comparing this machine's Beaconcha cache files
# with the validator_performances rows that earlier syncs wrote to the DB.
#
# READ-ONLY. The database is only ever queried with SELECT / COPY ... TO STDOUT,
# in a session switched to READ ONLY mode first (the server then rejects any
# write), and the script aborts if that mode is not active. Nothing is created
# in the database, not even temporary tables. The cache directory is only read.
# The only files written are under $OUT (on the machine running the script).
#
# Usage:
#   ./check-beaconcha-shift.sh [beaconcha_cache_dir]
#
# Environment (all optional):
#   POSTGRES   connection string (default: the docker-compose one,
#              postgres://user:1234@localhost:5435/ssv-rewards?sslmode=disable)
#   PSQL_CMD   full psql command to use instead, e.g. when Postgres only runs
#              inside docker compose:
#              PSQL_CMD='docker compose exec -T postgres psql -U user -d ssv-rewards'
#   SAMPLE     validators sampled per group (default 300)
#   SPLIT_DAY  first day considered "after the change" (default 2026-10-01)
#   OUT        output directory (default ./beaconcha-shift-check-<timestamp>)
set -euo pipefail

CACHE="${1:-./data/mainnet/.cache/beaconcha}"
POSTGRES="${POSTGRES:-postgres://user:1234@localhost:5435/ssv-rewards?sslmode=disable}"
PSQL_CMD="${PSQL_CMD:-psql \"\$POSTGRES\"}"
SAMPLE="${SAMPLE:-300}"
SPLIT_DAY="${SPLIT_DAY:-2026-10-01}"
OUT="${OUT:-./beaconcha-shift-check-$(date -u +%Y%m%dT%H%M%SZ)}"
export POSTGRES

die() { echo "ERROR: $*" >&2; exit 1; }
(( BASH_VERSINFO[0] >= 4 )) || die "bash >= 4 required"
for t in jq awk sort; do command -v "$t" >/dev/null || die "$t is required"; done
[[ -d "$CACHE" ]] || die "cache dir not found: $CACHE (pass it as the first argument)"
[[ "$SPLIT_DAY" =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}$ ]] || die "SPLIT_DAY must be YYYY-MM-DD"
[[ "$SAMPLE" =~ ^[0-9]+$ ]] || die "SAMPLE must be a number"

# Every session starts with this; the server then refuses any write in it.
READ_ONLY_SQL='SET SESSION CHARACTERISTICS AS TRANSACTION READ ONLY;'
export PGOPTIONS="${PGOPTIONS:-} -c default_transaction_read_only=on"  # belt and braces (host psql)
psql_run() { bash -c "$PSQL_CMD -X -q -v ON_ERROR_STOP=1 $*"; }
ro=$(printf '%s\nSHOW transaction_read_only;\n' "$READ_ONLY_SQL" | psql_run -At 2>/dev/null) ||
  die "cannot connect to Postgres; set POSTGRES or PSQL_CMD"
[[ "$ro" == "on" ]] || die "could not switch the session to read-only (got '$ro'); aborting"

mkdir -p "$OUT"
REPORT="$OUT/report.txt"
log() { echo "$*" | tee -a "$REPORT" >&2; }
log "beaconcha shift check — $(date -u +%FT%TZ)"
log "cache=$CACHE sample=$SAMPLE split_day=$SPLIT_DAY db_session_read_only=$ro"
log ""

# ---------------------------------------------------------------------------
# Step 1: census — fetch date of every cache file vs. time-of-day of day_start.
# Files are Go MarshalIndent output: "time" comes first, then data[0].day_start.
# ---------------------------------------------------------------------------
log "== Step 1: cache census (fetch date x day_start alignment)"
find "$CACHE" -maxdepth 1 -name '*.json' -print0 |
  xargs -0 -n 2000 awk '
    FNR == 1 { t = ""; done = 0 }
    done { next }
    t == "" && (p = index($0, "\"time\": \"")) > 0 { t = substr($0, p + 9, 10) }
    (p = index($0, "\"day_start\": \"")) > 0 {
      n = split(FILENAME, parts, "/"); idx = parts[n]; sub(/\.json$/, "", idx)
      print idx "\t" t "\t" substr($0, p + 24, 10)
      done = 1
    }' > "$OUT/census.tsv"
total_files=$(find "$CACHE" -maxdepth 1 -name '*.json' | wc -l)
log "files: $total_files  with data: $(wc -l < "$OUT/census.tsv")"
{ printf '%10s  %-10s  %s\n' count fetched day_start_time
  cut -f2,3 "$OUT/census.tsv" | sort | uniq -c | awk '{printf "%10s  %-10s  %s\n", $1, $2, $3}'
} | tee -a "$REPORT" >&2
log ""

awk -F'\t' '$3 == "T00:00:00Z" {print $1}' "$OUT/census.tsv" > "$OUT/new-format.idx"
awk -F'\t' '$3 == "T12:00:23Z" {print $1}' "$OUT/census.tsv" > "$OUT/old-format.idx"
log "new-format (UTC midnight) files: $(wc -l < "$OUT/new-format.idx")"
log "old-format (beacon day)  files: $(wc -l < "$OUT/old-format.idx")"
log ""

# ---------------------------------------------------------------------------
# Step 2: sample validators from each group and extract their cached days.
# ---------------------------------------------------------------------------
log "== Step 2: extracting cached days for $SAMPLE validators per group"
: > "$OUT/cache-sample.tsv"
for grp in new old; do
  # Deterministic pseudo-random sample (same seed => same validators on a re-run).
  awk 'BEGIN { srand(42) } { print rand() "\t" $0 }' "$OUT/$grp-format.idx" |
    sort -n | head -n "$SAMPLE" | cut -f2 > "$OUT/$grp-sample.idx"
  while read -r idx; do
    jq -r --arg g "$grp" --arg i "$idx" \
      '.data[] | [$g, $i, .day_start[0:10], .missed_attestations, .end_effective_balance] | @tsv' \
      "$CACHE/$idx.json"
  done < "$OUT/$grp-sample.idx" >> "$OUT/cache-sample.tsv"
  log "$grp: $(wc -l < "$OUT/$grp-sample.idx") validators"
done
log "cached validator-days extracted: $(wc -l < "$OUT/cache-sample.tsv")"
log ""

# ---------------------------------------------------------------------------
# Step 3: read the DB rows for the sampled validators (read-only session) and
# compare them with the cache locally.
# ---------------------------------------------------------------------------
log "== Step 3: DB vs cache comparison"
idx_list=$(cat "$OUT/new-sample.idx" "$OUT/old-sample.idx" | grep -E '^[0-9]+$' | paste -sd, - || true)
[[ -n "$idx_list" ]] || die "no sampled validators"
printf '%s\nCOPY (SELECT v.index, vp.day, vp.attestations_missed, vp.end_effective_balance FROM validators v JOIN validator_performances vp ON vp.provider = %s AND vp.public_key = v.public_key WHERE v.index IN (%s) AND vp.attestations_missed IS NOT NULL) TO STDOUT;\n' \
  "$READ_ONLY_SQL" "'beaconcha'" "$idx_list" | psql_run > "$OUT/db-sample.tsv"
log "DB validator-days read: $(wc -l < "$OUT/db-sample.tsv")"

awk -v split_day="$SPLIT_DAY" '
  # Days since 1970-01-01 for a YYYY-MM-DD string (civil calendar).
  function dn(s,   y, m, d, era, yoe, doy, doe) {
    y = substr(s, 1, 4) + 0; m = substr(s, 6, 2) + 0; d = substr(s, 9, 2) + 0
    if (m <= 2) y--
    era = int((y >= 0 ? y : y - 399) / 400); yoe = y - era * 400
    doy = int((153 * (m + (m > 2 ? -3 : 9)) + 2) / 5) + d - 1
    doe = yoe * 365 + int(yoe / 4) - int(yoe / 100) + doy
    return era * 146097 + doe - 719468
  }
  function pct(a, b) { return b ? sprintf("%.2f", 100 * a / b) : "-" }
  function eth(g) { return (g == "" ? "-" : int(g / 1000000000)) }
  function sample(arr, nvar, line,   r) {  # reservoir sample of 20 lines
    if (nvar <= 20) arr[nvar] = line
    else { r = int(rand() * nvar) + 1; if (r <= 20) arr[r] = line }
  }
  BEGIN { FS = OFS = "\t"; S = dn(split_day); srand(42) }
  FNR == NR { k = $2 SUBSEP dn($3); CM[k] = $4; CE[k] = $5; G[$2] = $1; next }
  {
    idx = $1; d = dn($2); k = idx SUBSEP d
    if (!(k in CM)) next
    g = G[idx]; per = (d >= S) ? "on/after " split_day : "before " split_day
    dbm = $3 + 0; dbe = $4; cm = CM[k] + 0; ce = CE[k] + 0
    mm = (dbm != cm); em = (dbe == "\\N" || dbe + 0 != ce); any = (mm || em)
    key = g SUBSEP per; KEYS[key] = 1
    N[key]++; MM[key] += mm; EM[key] += em; AM[key] += any
    if (!((key, idx) in VS)) { VS[key, idx] = 1; NV[key]++ }
    if (any && !((key, idx) in VX)) { VX[key, idx] = 1; NVX[key]++ }
    mon = substr($2, 1, 7); MON[mon] = 1; MN[mon, g]++; MA[mon, g] += any
    if (g == "new" && d < S) {
      kn = idx SUBSEP (d + 1); kp = idx SUBSEP (d - 1)
      if (mm && (kn in CM)) { T1n++; T1p += (dbm <= cm + CM[kn]) }
      if (mm && (kp in CM)) { T2n++; T2p += (dbm <= CM[kp] + cm) }
      if (dbe != "\\N" && dbe + 0 != ce) {
        e = dbe + 0
        if (kn in CE) { a = ce; b = CE[kn] + 0; lo = (a < b ? a : b); hi = (a < b ? b : a)
                        T3n++; T3p += (e >= lo && e <= hi); T5p += (e == b) }
        if (kp in CE) { a = CE[kp] + 0; b = ce; lo = (a < b ? a : b); hi = (a < b ? b : a)
                        T4n++; T4p += (e >= lo && e <= hi) }
      }
      if (any) {
        nex++
        sample(EX, nex, sprintf("%8s  %s  %9s  %6s  %6s  %6s  %9s  %8s  %13s", idx, $2, dbm,
               (kp in CM) ? CM[kp] : "-", cm, (kn in CM) ? CM[kn] : "-",
               eth(dbe == "\\N" ? "" : dbe), eth(ce), (kn in CE) ? eth(CE[kn]) : "-"))
      }
    }
    if (g == "old" && any) {
      nox++
      sample(OX, nox, sprintf("%8s  %s  %9s  %8s  %9s  %8s", idx, $2, dbm, cm, eth(dbe == "\\N" ? "" : dbe), eth(ce)))
    }
  }
  END {
    print ""
    print "-- 3a. Mismatch rates by group and period (days before / on-or-after SPLIT_DAY)"
    printf "%-4s  %-20s  %10s  %8s  %15s  %11s  %17s  %24s\n", "grp", "period", "validators", "days",
           "missed_mismatch", "eb_mismatch", "pct_days_mismatch", "validators_with_mismatch"
    for (g = 1; g <= 2; g++) for (p = 1; p <= 2; p++) {
      key = (g == 1 ? "new" : "old") SUBSEP (p == 1 ? "before " : "on/after ") split_day
      if (!(key in KEYS)) continue
      split(key, kk, SUBSEP)
      printf "%-4s  %-20s  %10d  %8d  %15d  %11d  %17s  %24d\n", kk[1], kk[2], NV[key], N[key],
             MM[key], EM[key], pct(AM[key], N[key]), NVX[key]
    }
    print ""
    print "-- 3b. Mismatch rate by month (percentage of compared days that differ)"
    printf "%-7s  %8s  %16s  %8s  %16s\n", "month", "new_days", "new_pct_mismatch", "old_days", "old_pct_mismatch"
    cmd = "sort"
    for (m in MON) printf "%-7s  %8d  %16s  %8d  %16s\n", m, MN[m, "new"], pct(MA[m, "new"], MN[m, "new"]),
                          MN[m, "old"], pct(MA[m, "old"], MN[m, "old"]) | cmd
    close(cmd)
    print ""
    print "-- 3c. Shift test on mismatching days of new-format validators before SPLIT_DAY."
    print "--     A beacon day D (D 12:00:23 -> D+1 12:00:23) lies inside UTC days D and D+1, so if the"
    print "--     DB holds beacon-day values and the cache holds UTC-day values, the forward checks"
    print "--     must pass ~100%. The [control] checks (UTC days D-1 and D) should be clearly lower."
    printf "%-56s  %6s  %8s\n", "test", "days", "pct_pass"
    printf "%-56s  %6d  %8s\n", "missed: db(D) <= cache(D) + cache(D+1)", T1n, pct(T1p, T1n)
    printf "%-56s  %6d  %8s\n", "missed: db(D) <= cache(D-1) + cache(D)  [control]", T2n, pct(T2p, T2n)
    printf "%-56s  %6d  %8s\n", "end EB: db(D) between cache(D) and cache(D+1)", T3n, pct(T3p, T3n)
    printf "%-56s  %6d  %8s\n", "end EB: db(D) between cache(D-1) and cache(D)  [control]", T4n, pct(T4p, T4n)
    printf "%-56s  %6d  %8s\n", "end EB: db(D) = cache(D+1) exactly", T3n, pct(T5p, T3n)
    print ""
    print "-- 3d. Examples (new-format validators, mismatching days before SPLIT_DAY; EB in ETH)"
    printf "%8s  %-10s  %9s  %6s  %6s  %6s  %9s  %8s  %13s\n", "idx", "day", "db_missed", "c_prev", "c_d", "c_next", "db_eb_eth", "c_eb_eth", "c_next_eb_eth"
    for (i = 1; i <= 20 && i <= nex; i++) print EX[i]
    print ""
    print "-- 3e. Control-group (old-format) days that mismatch: " (nox + 0) " (up to 20 shown)"
    if (nox) printf "%8s  %-10s  %9s  %8s  %9s  %8s\n", "idx", "day", "db_missed", "c_missed", "db_eb_eth", "c_eb_eth"
    for (i = 1; i <= 20 && i <= nox; i++) print OX[i]
  }' "$OUT/cache-sample.tsv" "$OUT/db-sample.tsv" | tee -a "$REPORT" >&2

log ""
log "Done. Send back the whole directory: $OUT  (report.txt is the summary)"
