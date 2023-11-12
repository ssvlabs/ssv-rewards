DROP FUNCTION IF EXISTS active_days_by_validator(provider_type, INTEGER, DATE, DATE);
DROP FUNCTION IF EXISTS active_days_by_owner(provider_type, INTEGER, DATE, DATE);
DROP FUNCTION IF EXISTS active_days_by_recipient(provider_type, INTEGER, DATE, DATE);
DROP FUNCTION IF EXISTS inactive_days_by_validator(provider_type, INTEGER, DATE, DATE);

CREATE OR REPLACE FUNCTION active_days_by_validator(_provider provider_type, min_daily_attestations INTEGER, from_period DATE, to_period DATE DEFAULT NULL)
RETURNS TABLE (
    owner_address TEXT,
    public_key TEXT,
    active_days BIGINT
) AS $$
BEGIN
    IF to_period IS NULL THEN
        to_period := from_period;
    END IF;

    RETURN QUERY
    SELECT
        vp.owner_address,
        vp.public_key,
        count(vp.*) AS active_days
    FROM validator_performances AS vp
    WHERE provider = _provider
        AND solvent_whole_day
        AND attestations_executed >= min_daily_attestations
        AND date_trunc('month', day) BETWEEN date_trunc('month', from_period) AND date_trunc('month', to_period)
    GROUP BY vp.owner_address, vp.public_key;
END;
$$ LANGUAGE plpgsql STABLE;

CREATE OR REPLACE FUNCTION active_days_by_owner(_provider provider_type, min_daily_attestations INTEGER, from_period DATE, to_period DATE DEFAULT NULL)
RETURNS TABLE (
    owner_address TEXT,
    validators BIGINT,
    active_days BIGINT
) AS $$
BEGIN
    RETURN QUERY
    SELECT
        dr.owner_address,
        COUNT(dr.public_key) AS number_of_validators,
        SUM(dr.active_days)::BIGINT AS active_days
    FROM active_days_by_validator(_provider, min_daily_attestations, from_period, to_period) dr
    GROUP BY dr.owner_address;
END;
$$ LANGUAGE plpgsql STABLE;

CREATE OR REPLACE FUNCTION active_days_by_recipient(_provider provider_type, min_daily_attestations INTEGER, from_period DATE, to_period DATE DEFAULT NULL)
RETURNS TABLE (
    recipient_address TEXT,
    is_deployer BOOLEAN,
    validators BIGINT,
    active_days BIGINT
) AS $$
BEGIN
    RETURN QUERY
    SELECT
        COALESCE(d.deployer_address, ado.owner_address) AS recipient_address,
        BOOL_OR(d.deployer_address IS NOT NULL) AS is_deployer,
        SUM(ado.validators)::BIGINT AS validators,
        SUM(ado.active_days)::BIGINT AS active_days
    FROM active_days_by_owner(_provider, min_daily_attestations, from_period, to_period) ado
    LEFT JOIN deployers d ON ado.owner_address = d.owner_address
    GROUP BY COALESCE(d.deployer_address, ado.owner_address);
END;
$$ LANGUAGE plpgsql STABLE;

CREATE OR REPLACE FUNCTION inactive_days_by_validator(_provider provider_type, min_daily_attestations INTEGER, from_period DATE, to_period DATE default NULL)
RETURNS TABLE (
	day DATE,
	from_epoch INTEGER,
	to_epoch INTEGER,
    owner_address TEXT,
    public_key TEXT,
    start_beacon_status TEXT,
    end_beacon_status TEXT,
    events TEXT,
    exclusion_reason TEXT
) AS $$
BEGIN
    IF to_period IS NULL THEN
        to_period := from_period;
    END IF;

    RETURN QUERY
    SELECT
    	vp.day,
    	vp.from_epoch,
    	vp.to_epoch,
        vp.owner_address,
        vp.public_key,
        vp.start_beacon_status,
        vp.end_beacon_status,
        (
            SELECT string_agg(ve.event_name, ', ') -- Aggregates event names separated by commas
            FROM validator_events AS ve
            WHERE ve.public_key = vp.public_key
              AND (ve.slot/32) BETWEEN vp.from_epoch AND vp.to_epoch
        ) AS events,
        CASE
            WHEN NOT vp.solvent_whole_day THEN 'not_registered_whole_day'
            WHEN vp.attestations_executed < min_daily_attestations THEN 'not_enough_attestations'
            ELSE 'unknown'
        END AS exclusion_reason
    FROM validator_performances AS vp
    WHERE provider = _provider
        AND date_trunc('month', vp.day) BETWEEN date_trunc('month', from_period) AND date_trunc('month', to_period)
        AND (NOT solvent_whole_day OR attestations_executed < min_daily_attestations);
END;
$$ LANGUAGE plpgsql STABLE;

select * from active_days_by_owner('beaconcha', 202, '2023-07-01', '2023-10-01');