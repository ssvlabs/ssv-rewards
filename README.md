# ssv-rewards

Synchronizes historical activity and performance of SSV validators and calculates their rewards according to [Incentivized Mainnet Program](https://docs.google.com/document/d/1pcr8QVcq9eZfiOJGrm5OsE9JAqdQy1F8Svv1xgecjNY).

## Installation

```bash
git clone https://github.com/bloxapp/ssv-rewards
cd ssv-rewards
cp .env.example .env
cp rewards.example.yaml rewards.yaml
```

Edit `.env` and fill in the required values:

```ini
# SSV network
NETWORK=mainnet

# Beacon API endpoint of the consensus node
CONSENSUS_ENDPOINT=http://beacon-node:5052

# JSON-RPC API endpoint of the execution node
EXECUTION_ENDPOINT=http://excution-node:8545

# SSV API endpoint
SSV_API_ENDPOINT=https://api.ssv.network/api/v4

# Beaconcha.in API
BEACONCHA_ENDPOINT=https://beaconcha.in
BEACONCHA_API_KEY= # Optional
BEACONCHA_REQUESTS_PER_MINUTE=20 # Adjust according to your Beaconcha.in API plan
```

Edit `rewards.yaml` to match [the specifications](https://docs.google.com/document/d/1pcr8QVcq9eZfiOJGrm5OsE9JAqdQy1F8Svv1xgecjNY):

```yaml
version: 2

mechanics:
  - since: 2023-07
    criteria:
      min_attestations_per_day: 202
      min_decideds_per_day: 22    
    tiers:
      - max_participants: 2000 # Effective at up to 2,000 validators
        apr_boost: 0.5 # Fraction of ETH APR to reward in SSV tokens
      # ...
      - max_participants: 30000
        apr_boost: 0.1
  - since: 2023-11
    tiers:
      - max_participants: 2000
        apr_boost: 0.25
      # ...
      - max_participants: 30000
        apr_boost: 0.05
    # Redirect rewards to different addresses.
    # The left-hand side is the owner address, and the right-hand side is the reward recipient address.
    owner_redirects:
      "0x1234567890abcdef1234567890abcdef12345678": "0x1234567890abcdef1234567890abcdef12345678"
    # Redirect rewards to different addresses by validator public key.
    # The left-hand side is the validator public key, and the right-hand side is the reward recipient address.
    validator_redirects:
      "0x1234500012345000123450001234500012345000123450001234500012345000123450001234500012345000123450001234": "0x1234567890abcdef1234567890abcdef12345678"

    # Alternatively, you can specify redirects using external CSV files:
    # - You cannot use both `owner_redirects` and `owner_redirects_file` simultaneously. Choose one method.
    # - You cannot use both `validator_redirects` and `validator_redirects_file` simultaneously. Choose one method.
    # - Each file must have a header row with "from" and "to" as column names.

    # For owner redirects, the "from" column contains owner addresses, and the "to" column contains recipient addresses.
    # Example of owner_redirects_file content:
    # from,to
    # 0x1234567890abcdef1234567890abcdef12345678,0xabcdefabcdefabcdefabcdefabcdefabcdefabcdef
    # 0xabcdefabcdefabcdefabcdefabcdefabcdefabcdef,0x1234567890abcdef1234567890abcdef12345678
    owner_redirects_file: owner_redirects_2023_11.csv
    # For validator redirects, the "from" column contains validator public keys, and the "to" column contains recipient addresses.
    # Example of validator_redirects_file content:
    # from,to
    # 0x1234500012345000123450001234500012345000123450001234500012345000123450001234500012345000123450001234,0x1234567890abcdef1234567890abcdef12345678
    # 0xabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdef,0xabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdef
    validator_redirects_file: validator_redirects_2023_11.csv

rounds:
  - period: 2023-07 # Designated period (year-month)
    eth_apr: 0.047 # ETH Staking APR
    ssv_eth: 0.0088235294 # SSV/ETH price
  # ...
```

## Usage

First, start PostgreSQL and wait a few seconds for it to be ready:

```bash
docker-compose up -d postgres
```

### Synchronization

Synchronize validator activity and performance:

```bash
docker-compose run --rm sync
```

_This should sync validator performance up until 2 days ago (UTC) or until the end of the last period in `rewards.yaml` (whichever is lower). Therefore, in order to sync the last month, waiting until the 3rd of this month is required._

_This might take a while, depending on how long ago the SSV contract was deployed and how many validators there are._

### Calculation

After syncing, you may calculate the reward distribution:

```bash
docker-compose run --rm calc
```

This produces the following documents under the `./rewards` directory:

```bash
📂 rewards
├── 📄 by-owner.csv            # Reward for each owner for each round
├── 📄 by-validator.csv        # Reward for each validator for each round
├── 📄 by-recipient.csv        # Reward for each recipient for each round
├── 📄 total-by-owner.csv      # Total reward for each owner
├── 📄 total-by-validator.csv  # Total reward for each validator
└── 📂 <year>-<month>
    ├── 📄 by-owner.csv        # Total reward for each owner for that round
    ├── 📄 by-validator.csv    # Total reward for each validator for that round
    ├── 📄 by-recipient.csv    # Total reward for each recipient for that round
    └── 📄 cumulative.json     # Cumulative reward for each owner until and including that round
```

- `recipient` is the address that eventually receives the reward, which is either the owner address, or if the owner is redirecting the reward, the address specified in `owner_redirects` or `owner_redirects_file`.

### Merkleization

After calculating the reward distribution, you may merkleize the rewards for a specific round.

1. Copy the file at `./rewards/<year>-<month>/cumulative.json` over to `./scripts/merkle-generator/scripts/input_1.json`.
2. Run the merkleization script:
   ```bash
   cd scripts/merkle-generator
   npm i
   npx hardhat run scripts/merkle.ts
   ```
3. The merkle tree is generated at `./merkle-generator/scripts/output-1.json`.

## Updating

1. Pull the changes and rebuild the Docker images:
   ```bash
   git pull
   docker-compose build
   ```
2. Refer to `.env.example` and update your `.env` file if necessary.
3. Refer to `rewards.example.yaml` and update your `rewards.yaml` file if necessary.
4. Sync with `--fresh` to re-create the databases and sync from scratch:
   ```bash
   docker-compose run --rm sync sync --fresh
   ```
