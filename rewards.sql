CREATE OR REPLACE FUNCTION participations_by_validator(
    _provider provider_type,
    min_attestations INTEGER,
    min_decideds INTEGER,
    from_period DATE,
    to_period DATE DEFAULT NULL,
    owner_redirects_support BOOLEAN DEFAULT FALSE,
    validator_redirects_support BOOLEAN DEFAULT FALSE
)
RETURNS TABLE (
    recipient_address TEXT,
    owner_address TEXT,
    public_key TEXT,
    active_days BIGINT,
    registered_days BIGINT,
    active_effective_balance BIGINT,
    registered_effective_balance BIGINT
) AS $$
DECLARE
    _from_month DATE := date_trunc('month', from_period);
    _to_month DATE := date_trunc('month', COALESCE(to_period, from_period));
BEGIN
    RETURN QUERY
        WITH vp_redirected AS (
        SELECT
            vp.owner_address,
            vp.public_key,
            GREATEST(vp.end_effective_balance, 32000000000) AS end_effective_balance,
            COALESCE(
                CASE WHEN validator_redirects_support THEN vr.to_address END,
                CASE WHEN owner_redirects_support THEN owr.to_address END,
                vp.owner_address
            ) AS recipient_address,
            ((vp.attestations_executed >= min_attestations) AND (vp.decideds >= min_decideds))::BOOLEAN AS is_active
        FROM validator_performances vp
        LEFT JOIN validator_redirects vr ON validator_redirects_support AND vp.public_key = vr.public_key
        LEFT JOIN owner_redirects owr ON owner_redirects_support AND vp.owner_address = owr.from_address
        WHERE vp.provider = _provider
          AND vp.day >= _from_month AND vp.day < (_to_month + INTERVAL '1 month')
          AND vp.solvent_whole_day
    )
    SELECT
        vpr.recipient_address,
        vpr.owner_address,
        vpr.public_key,
        COUNT(*) FILTER (WHERE vpr.is_active) AS active_days,
        COUNT(*) AS registered_days,
        COALESCE(SUM(end_effective_balance) FILTER (WHERE vpr.is_active), 0)::BIGINT AS active_effective_balance,
        COALESCE(SUM(end_effective_balance), 0)::BIGINT AS registered_effective_balance
    FROM vp_redirected vpr
    GROUP BY vpr.recipient_address, vpr.owner_address, vpr.public_key
    HAVING COUNT(*) FILTER (WHERE is_active) > 0;
END;
$$ LANGUAGE plpgsql STABLE;

CREATE OR REPLACE FUNCTION participations_by_recipient(
    _provider provider_type,
    min_attestations INTEGER,
    min_decideds INTEGER,
    from_period DATE,
    to_period DATE DEFAULT NULL,
    owner_redirects_support BOOLEAN DEFAULT FALSE,
    validator_redirects_support BOOLEAN DEFAULT FALSE
)
RETURNS TABLE (
    recipient_address TEXT,
    validators BIGINT,
    active_days BIGINT,
    registered_days BIGINT,
    active_effective_balance BIGINT,
    registered_effective_balance BIGINT
) AS $$
BEGIN
    RETURN QUERY
    SELECT
        adv.recipient_address,
        COUNT(adv.public_key) AS validators,
        SUM(adv.active_days)::BIGINT AS active_days,
        SUM(adv.registered_days)::BIGINT AS registered_days,
        SUM(adv.active_effective_balance)::BIGINT AS active_effective_balance,
        SUM(adv.registered_effective_balance)::BIGINT AS registered_effective_balance
    FROM participations_by_validator(
        _provider,
        min_attestations,
        min_decideds,
        from_period,
        to_period,
        owner_redirects_support,
        validator_redirects_support
    ) adv
    GROUP BY adv.recipient_address;
END;
$$ LANGUAGE plpgsql STABLE;

CREATE OR REPLACE FUNCTION participations_by_owner(
    _provider provider_type,
    min_attestations INTEGER,
    min_decideds INTEGER,
    from_period DATE,
    to_period DATE DEFAULT NULL,
    owner_redirects_support BOOLEAN DEFAULT FALSE,
    validator_redirects_support BOOLEAN DEFAULT FALSE
)
RETURNS TABLE (
    recipient_address TEXT,
    owner_address TEXT,
    validators BIGINT,
    active_days BIGINT,
    registered_days BIGINT,
    active_effective_balance BIGINT,
    registered_effective_balance BIGINT
) AS $$
BEGIN
    RETURN QUERY
    SELECT
        adv.recipient_address,
        adv.owner_address,
        COUNT(adv.public_key) AS validators,
        SUM(adv.active_days)::BIGINT AS active_days,
        SUM(adv.registered_days)::BIGINT AS registered_days,
        SUM(adv.active_effective_balance)::BIGINT AS active_effective_balance,
        SUM(adv.registered_effective_balance)::BIGINT AS registered_effective_balance
    FROM participations_by_validator(
        _provider,
        min_attestations,
        min_decideds,
        from_period,
        to_period,
        owner_redirects_support,
        validator_redirects_support
    ) adv
    GROUP BY adv.owner_address, adv.recipient_address;
END;
$$ LANGUAGE plpgsql STABLE;

CREATE OR REPLACE FUNCTION exclusions_by_validator(
    _provider provider_type,
    min_attestations INTEGER,
    min_decideds INTEGER,
    from_period DATE,
    to_period DATE default NULL
)
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
DECLARE
    _from_month DATE := date_trunc('month', from_period);
    _to_month DATE := date_trunc('month', COALESCE(to_period, from_period));
BEGIN
    RETURN QUERY
    WITH vp_excluded AS (
        SELECT
            vp.day,
            vp.from_epoch,
            vp.to_epoch,
            vp.owner_address,
            vp.public_key,
            vp.start_beacon_status,
            vp.end_beacon_status,
            CASE
                WHEN NOT vp.solvent_whole_day THEN 'not_registered_whole_day'
                WHEN vp.attestations_executed < min_attestations THEN 'not_enough_attestations'
                WHEN vp.decideds < min_decideds THEN 'not_enough_decideds'
                ELSE 'unknown'
            END AS exclusion_reason
        FROM validator_performances AS vp
        WHERE provider = _provider
          AND vp.day >= _from_month AND vp.day < (_to_month + INTERVAL '1 month')
          AND (NOT solvent_whole_day OR attestations_executed < min_attestations OR decideds < min_decideds)
    )
    SELECT
    	v.day,
    	v.from_epoch,
    	v.to_epoch,
        v.owner_address,
        v.public_key,
        v.start_beacon_status,
        v.end_beacon_status,
        (
            SELECT string_agg(ve.event_name, ', ') -- Aggregates event names separated by commas
            FROM validator_events AS ve
            WHERE ve.public_key = v.public_key
              AND (ve.slot/32) BETWEEN v.from_epoch AND v.to_epoch
        ) AS events,
        v.exclusion_reason
    FROM vp_excluded AS v;
END;
$$ LANGUAGE plpgsql STABLE;
