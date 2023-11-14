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

# Etherscan API
ETHERSCAN_API_ENDPOINT=https://api.etherscan.io
ETHERSCAN_API_KEY= # Optional
ETHERSCAN_REQUESTS_PER_SECOND=0.1 # Adjust according to your Etherscan API plan
```

Edit `rewards.yaml` to match [the specifications](https://docs.google.com/document/d/1pcr8QVcq9eZfiOJGrm5OsE9JAqdQy1F8Svv1xgecjNY):

```yaml
criteria:
  min_attestations_per_day: 202
  min_decideds_per_day: 22

tiers:
  # Tiers apply to rounds below the participation threshold.
  - max_participants: 2000 # Up to 2,000 validators
    apr_boost: 0.5 # Fraction of ETH APR to reward in SSV tokens
  # ...
  - max_participants: 30000
    apr_boost: 0.1

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

- `recipient` is the address that eventually receives the reward, which is either the owner address, or if the owner is a contract, then the deployer address of the contract.

### Merkleization

After calculating the reward distribution, you may merkleize the rewards for a specific round.

1. Copy the file at `./rewards/<year>-<month>/cumulative.json` over to `./scripts/merkle-generator/scripts/input-1.json`.
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
