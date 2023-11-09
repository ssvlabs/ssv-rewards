-- Get tier reward based on the total number of active validators
CREATE OR REPLACE FUNCTION get_tier_reward(provider_name provider, month DATE)
RETURNS NUMERIC AS $$
DECLARE
    total_validators_count INTEGER;
    reward NUMERIC;
BEGIN
    SELECT COUNT(DISTINCT public_key)
    INTO total_validators_count
    FROM validator_performances
    WHERE provider = provider_name
        AND date_trunc('month', day) = date_trunc('month', month);

    CASE
        WHEN total_validators_count BETWEEN 1 AND 2000 THEN reward := 7.10;
        WHEN total_validators_count BETWEEN 2001 AND 5000 THEN reward := 5.68;
        WHEN total_validators_count BETWEEN 5001 AND 10000 THEN reward := 4.26;
        WHEN total_validators_count BETWEEN 10001 AND 15000 THEN reward := 2.84;
        WHEN total_validators_count >= 15001 THEN reward := 1.42;
        ELSE reward := 0;
    END CASE;

    RETURN reward;
END;
$$ LANGUAGE plpgsql STABLE;

-- Get the number of days in the given month
CREATE OR REPLACE FUNCTION get_days_in_month(month DATE)
RETURNS INTEGER AS $$
BEGIN
    RETURN EXTRACT(DAY FROM (DATE_TRUNC('month', month) + INTERVAL '1 MONTH - 1 day'));
END;
$$ LANGUAGE plpgsql STABLE;

-- Calculate detailed rewards by validator
CREATE OR REPLACE FUNCTION calculate_detailed_rewards(provider_name provider, minimum_attestations_per_day INTEGER, month DATE)
RETURNS TABLE (
    owner_address TEXT,
    public_key TEXT,
    accrued_days INTEGER,
    tier_ssv_reward NUMERIC,
    days_in_round INTEGER,
    ssv_reward NUMERIC
) AS $$
DECLARE
    tier_reward NUMERIC := get_tier_reward(provider_name, month);
    days_in_month INTEGER := get_days_in_month(month);
BEGIN
    RETURN QUERY
    SELECT
        vp.owner_address,
        vp.public_key,
        count(vp.*)::INTEGER AS accrued_days,
        tier_reward AS tier_ssv_reward,
        days_in_month AS days_in_round,
        (tier_reward * count(*)::NUMERIC / days_in_month) AS ssv_reward
    FROM validator_performances AS vp
    WHERE provider = provider_name
        AND active_whole_day
        AND attestations_executed >= minimum_attestations_per_day
        AND date_trunc('month', day) = date_trunc('month', month)
    GROUP BY vp.owner_address, vp.public_key;
END;
$$ LANGUAGE plpgsql STABLE;

-- Calculate total rewards by owner address
CREATE OR REPLACE FUNCTION calculate_final_rewards(provider_name provider, minimum_attestations_per_day INTEGER, month DATE)
RETURNS TABLE (
    owner_address TEXT,
    number_of_validators INTEGER,
    total_accrued_days INTEGER,
    total_ssv_reward NUMERIC
) AS $$
BEGIN
    RETURN QUERY
    SELECT
        dr.owner_address,
        COUNT(dr.public_key)::INTEGER AS number_of_validators,
        SUM(dr.accrued_days)::INTEGER AS total_accrued_days,
        SUM(dr.ssv_reward) AS total_ssv_reward
    FROM calculate_detailed_rewards(provider_name, minimum_attestations_per_day, month) dr
    GROUP BY dr.owner_address;
END;
$$ LANGUAGE plpgsql STABLE;

-- Get inactive days for each validator and the reason for exclusion
CREATE OR REPLACE FUNCTION get_inactive_days(provider_name provider, minimum_attestations_per_day INTEGER, month DATE)
RETURNS TABLE (
	day DATE,
	from_epoch INTEGER,
	to_epoch INTEGER,
    owner_address TEXT,
    public_key TEXT,
    start_beacon_status TEXT,
    end_beacon_status TEXT,
    exclusion_reason TEXT
) AS $$
BEGIN
    RETURN QUERY
    SELECT
    	vp.day,
    	vp.from_epoch,
    	vp.to_epoch,
        vp.owner_address,
        vp.public_key,
        vp.start_beacon_status,
        vp.end_beacon_status,
        CASE
            WHEN NOT vp.active_whole_day THEN 'not_registered_whole_day'
            WHEN vp.attestations_executed < minimum_attestations_per_day THEN 'not_enough_attestations'
            ELSE 'unknown'
        END AS exclusion_reason
    FROM validator_performances AS vp
    WHERE provider = provider_name
        AND date_trunc('month', vp.day) = date_trunc('month', month)
        AND (NOT active_whole_day OR attestations_executed < minimum_attestations_per_day);
END;
$$ LANGUAGE plpgsql STABLE;

-- SELECT * FROM get_inactive_days('beaconcha', 202, DATE '2023-10-01');