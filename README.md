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

# Beaconcha.in API
BEACONCHA_ENDPOINT=https://beaconcha.in
BEACONCHA_API_KEY=<your-api-key>

# Optional: If you have a paid plan, you can increase the rate limit.
# BEACONCHA_REQUESTS_PER_MINUTE=100
```

Edit `rewards.yaml` to match [the specifications](https://docs.google.com/document/d/1pcr8QVcq9eZfiOJGrm5OsE9JAqdQy1F8Svv1xgecjNY):

```yaml
tiers:
  # Tiers apply to rounds below the participation threshold.
  - max_participants: 2000 # Up to 2,000 validators
    apr_boost: 0.5 # Fraction of ETH APR to reward in SSV tokens
  # ...
  - max_participants: ~ # Limitless
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

_This might take a while, depending on how long ago the SSV contract was deployed._

### Calculation

After syncing, you may calculate the reward distribution for a given period:

```bash
docker-compose run --rm calc
```

This produces the following documents under the `./rewards` directory:

```bash
📂 rewards
├── 📄 by-owner.csv            # Reward per round for each owner
├── 📄 by-validator.csv        # Reward per round for each validator
├── 📄 total-by-owner.csv      # Total reward for each owner
├── 📄 total-by-validator.csv  # Total reward for each validator
└── 📂 <year>-<month>
    ├── 📄 by-owner.csv        # Total reward for each owner for this month
    ├── 📄 by-validator.csv    # Total reward for each validator for this month
    └── 📄 cumulative.json     # Cumulative reward for each owner until and including this month
```

### Merkleization

TODO: After calculating the distribution, you may merkleize the rewards for each month.
