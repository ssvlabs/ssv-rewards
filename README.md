# ssv-rewards

Synchronizes historical activity and performance of SSV validators and calculates their rewards according to [Incentivized Mainnet Program](https://docs.google.com/document/d/1pcr8QVcq9eZfiOJGrm5OsE9JAqdQy1F8Svv1xgecjNY/edit).

## Installation

```bash
git clone https://github.com/bloxapp/ssv-rewards
cd ssv-rewards
cp .env.example .env
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

After syncing, you may calculate rewards for a given period:

```bash
docker-compose run --rm calc --from 2023-06 --to 2023-10
```

This calculates the reward distribution for the period from June 2023 to October 2023 (inclusive) and produces the following documents under the `./rewards` directory:

```bash
📂 rewards
├── 📄 by-owner.csv            # Daily rewards for each owner address
├── 📄 by-validator.csv        # Daily rewards for each validator
├── 📄 total-by-owner.csv      # Total reward for each owner address
├── 📄 total-by-validator.csv  # Total reward for each validator
└── 📂 <year>-<month>
    ├── 📄 by-owner.csv        # Daily rewards for each owner address for the month
    ├── 📄 by-validator.csv    # Daily rewards for each validator for the month
    └── 📄 merkle-tree.json    # Merkle tree for the month
```
