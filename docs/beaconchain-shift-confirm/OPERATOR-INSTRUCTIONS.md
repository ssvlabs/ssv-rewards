# Beaconcha cache vs DB check — operator instructions

We think beaconcha.in recently changed its daily validator stats from beacon-chain days
(starting 12:00:23 UTC) to UTC days (starting 00:00:00 UTC), and applied that to all
past data. Your machine ran this month's sync incrementally (without `--fresh`). So its
DB should still hold the old beacon-day values for past months, while any Beaconcha
cache file fetched recently holds the new UTC-day values. The script below compares
the two.

**Nothing here writes to the database.** The script, and the backup in step 0, only
read from Postgres:
- Every database session is switched to read-only mode before any query, so the
  server itself would reject a write. The script aborts if that mode isn't active.
- The script runs only `SELECT` / `COPY … TO STDOUT` and creates nothing in the
  database, not even temporary tables.
- The cache directory is only read.
- The only files written are the output folder and the backup files, on local disk.

## 0. Before you start (important)

1. **Do not run `sync` or `calc`** on this machine until we've reviewed the results.
   Never run `sync --fresh`. A sync can refetch Beaconcha data and overwrite the
   cache files being checked.
2. **Take a backup first.** This DB may be the last copy of the original beacon-day
   performance data. Run these from the ssv-rewards checkout and adjust the
   connection details if yours differ.

   `pg_dump` only reads: it works inside one read-only snapshot, and
   `PGOPTIONS=… read_only=on` additionally makes the server refuse any write in its
   session. While it runs it holds shared locks, so don't start a sync until it
   finishes. Expect about 7–8 GB, based on a similar database of 61 GB.

   If Postgres runs in docker compose:

   ```bash
   docker compose exec -T -e PGOPTIONS='-c default_transaction_read_only=on' postgres pg_dump -U user -Fc ssv-rewards > ssv-rewards-db-$(date -u +%F).dump
   ```

   If `pg_dump` is installed on the host:

   ```bash
   PGOPTIONS='-c default_transaction_read_only=on' pg_dump -Fc 'postgres://user:1234@localhost:5435/ssv-rewards?sslmode=disable' > ssv-rewards-db-$(date -u +%F).dump
   ```

   Then archive the Beaconcha cache. This only reads the files; expect about 3–5 GB.

   ```bash
   tar czf beaconcha-cache-$(date -u +%F).tar.gz -C data/mainnet/.cache beaconcha
   ```

   Keep both files.

## 1. Requirements

- `bash` 4 or newer, `jq`, `awk`, `sort`, `find`, `xargs` (all standard on Linux)
- `psql` on the host, **or** Postgres running in docker compose (see step 3)
- About 10 minutes. Most of the time goes on the first step, which reads every cache file.

## 2. Copy the script

Put `check-beaconcha-shift.sh` in the root of the ssv-rewards checkout (the folder that
contains `data/`), then:

```bash
chmod +x check-beaconcha-shift.sh
```

## 3. Run it

Pick the line that matches how Postgres runs on your machine.

Postgres from this repo's `docker-compose.yml` (port 5435 on the host) and `psql` installed on the host:

```bash
./check-beaconcha-shift.sh data/mainnet/.cache/beaconcha
```

Postgres inside docker compose, no `psql` on the host:

```bash
PSQL_CMD='docker compose exec -T postgres psql -U user -d ssv-rewards' ./check-beaconcha-shift.sh data/mainnet/.cache/beaconcha
```

Any other database (set your own connection string):

```bash
POSTGRES='postgres://USER:PASS@HOST:PORT/ssv-rewards?sslmode=disable' ./check-beaconcha-shift.sh /path/to/data/mainnet/.cache/beaconcha
```

Optional settings:
- `SAMPLE=300`: validators sampled per group. Raise it for more confidence.
- `SPLIT_DAY=2026-10-01`: first day whose DB rows were written *after* the change. Leave
  the default unless this machine's September sync ran after 20 Oct 2026; if it did,
  tell us.

## 4. Send back

The script ends by printing `Done. Send back the whole directory: ./beaconcha-shift-check-<timestamp>`. Archive that folder and send it to us:

```bash
tar czf beaconcha-shift-check.tar.gz beaconcha-shift-check-*/
```

`report.txt` in that folder is the summary. The other files are the raw inputs behind it.

Also tell us roughly when this machine's syncs ran over the last two months (dates).

---

## For the reviewer: how to read `report.txt`

**Step 1, census.** This counts cache files by fetch date and by the time of day
`day_start` uses.

| Result | Meaning |
|---|---|
| Files fetched before ~2026-10-20 show `T12:00:23Z`, files fetched after show `T00:00:00Z`, with no exceptions | The format switch is confirmed on this machine too |

**3a / 3b, mismatch rates.** "new" means validators whose cache file uses UTC days;
"old" means validators whose file still uses beacon days and serves as the control.

| Group / period | Expected if the hypothesis holds |
|---|---|
| new, days before `SPLIT_DAY` | Many days differ from the DB, spread over every past month |
| new, days on/after `SPLIT_DAY` | ~0% (those DB rows were written from the new data) |
| old, all days | ~0% (DB and cache come from the same beacon-day data) |

**3c, shift test.** This is the part that can falsify the hypothesis. A beacon day D
runs from D 12:00:23 to D+1 12:00:23 UTC, so it lies entirely inside UTC days D and D+1.

| Test | Expected |
|---|---|
| `missed: db(D) <= cache(D) + cache(D+1)` | ~100% |
| `end EB: db(D) between cache(D) and cache(D+1)` | ~100% |
| the two `[control]` rows (UTC days D-1 and D) | clearly lower |

**The hypothesis is wrong, or incomplete, if:** the "old" group shows real mismatches;
the forward checks in 3c fall well below 100% while the controls don't; or the census
shows UTC-midnight files fetched before October 2026.
