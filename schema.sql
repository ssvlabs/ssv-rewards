-- PostgreSQL schema for ssv-rewards.
CREATE TABLE IF NOT EXISTS state (
	id SERIAL PRIMARY KEY,
	network_name TEXT NOT NULL,
	lowest_block_number INTEGER NOT NULL,
	highest_block_number INTEGER NOT NULL,
	earliest_validator_performance DATE,
	latest_validator_performance DATE
);

CREATE TABLE IF NOT EXISTS contract_events (
	id SERIAL PRIMARY KEY,
	event_name TEXT NOT NULL,
	slot integer NOT NULL,
	block_number INTEGER NOT NULL,
	block_hash TEXT NOT NULL,
	block_time TIMESTAMP NOT NULL,
	transaction_hash TEXT NOT NULL,
	transaction_index INTEGER NOT NULL,
	log_index INTEGER NOT NULL,
	raw_log JSONB NOT NULL,
	raw_event JSONB NOT NULL,
	error TEXT,
	UNIQUE (block_number, log_index)
);

CREATE TABLE IF NOT EXISTS validators (
	public_key TEXT NOT NULL,
	index INT,
	active BOOLEAN NOT NULL,
	beacon_status TEXT,
	beacon_effective_balance BIGINT,
	beacon_activation_eligibility_epoch INT,
	beacon_activation_epoch INT,
	beacon_exit_epoch INT,
	beacon_slashed BOOLEAN,
	beacon_withdrawable_epoch INT,
	PRIMARY KEY (public_key)
);

CREATE TABLE IF NOT EXISTS validator_events (
    id SERIAL PRIMARY KEY,
    contract_event_id INTEGER NOT NULL REFERENCES contract_events(id),
	slot integer NOT NULL,
	block_number INTEGER NOT NULL,
	block_time TIMESTAMP NOT NULL,
	log_index INTEGER NOT NULL,
    public_key TEXT NOT NULL REFERENCES validators(public_key),
	owner_address TEXT NOT NULL,
    event_name TEXT NOT NULL,
    activated BOOLEAN NOT NULL,
	UNIQUE (block_number, log_index, owner_address, public_key)
);

CREATE TABLE IF NOT EXISTS owner_redirects (
	from_address TEXT NOT NULL,
	to_address TEXT NOT NULL,
	PRIMARY KEY (from_address)
);

CREATE TABLE IF NOT EXISTS validator_redirects (
    public_key TEXT NOT NULL REFERENCES validators(public_key),
    to_address TEXT NOT NULL,
    PRIMARY KEY (public_key)
);

CREATE INDEX IF NOT EXISTS idx_validator_events_public_key ON validator_events(public_key);

DO $$ BEGIN
    CREATE TYPE provider_type AS ENUM ('e2m', 'beaconcha');
EXCEPTION
    WHEN duplicate_object THEN null;
END $$;

CREATE TABLE IF NOT EXISTS validator_performances (
	provider provider_type NOT NULL,
	day DATE NOT NULL,
	from_epoch INT NOT NULL,
	to_epoch INT NOT NULL,
	owner_address TEXT NOT NULL,
	public_key TEXT NOT NULL REFERENCES validators(public_key),
	solvent_whole_day BOOLEAN NOT NULL,
	index INT,
    end_effective_balance BIGINT,
	start_beacon_status TEXT,
	end_beacon_status TEXT,
	decideds INT,

    effectiveness REAL,
	attestation_rate REAL,
	attestations_assigned SMALLINT,
	attestations_executed SMALLINT,
	attestations_missed SMALLINT,

	proposals_assigned SMALLINT,
	proposals_executed SMALLINT,
	proposals_missed SMALLINT,

	sync_committee_assigned SMALLINT,
	sync_committee_executed SMALLINT,
	sync_committee_missed SMALLINT,
	
	PRIMARY KEY (provider, day, public_key)
);


CREATE INDEX IF NOT EXISTS idx_validator_performances ON validator_performances(provider, day, owner_address, public_key, solvent_whole_day);