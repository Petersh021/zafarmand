-- Alternative administrator authentication remains anchored to the existing
-- active admin_users rows. A bounded per-user rate row survives challenge
-- rotation, OTP codes are represented only by keyed MACs, and durable OIDC
-- bindings retain the provider's immutable issuer/subject pair.
CREATE TABLE public.admin_email_otp_rate_limits (
    user_id bigint PRIMARY KEY,
    window_started_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    failed_attempt_count smallint NOT NULL DEFAULT 0,
    request_count smallint NOT NULL DEFAULT 0,
    last_requested_at timestamptz,
    updated_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT admin_email_otp_rate_limits_user_id_foreign
        FOREIGN KEY (user_id)
        REFERENCES public.admin_users (id)
        ON DELETE CASCADE,
    CONSTRAINT admin_email_otp_rate_limits_failed_attempt_count_supported
        CHECK (failed_attempt_count BETWEEN 0 AND 10),
    CONSTRAINT admin_email_otp_rate_limits_request_count_supported
        CHECK (request_count BETWEEN 0 AND 6),
    CONSTRAINT admin_email_otp_rate_limits_request_order
        CHECK (
            last_requested_at IS NULL OR
            last_requested_at >= window_started_at
        ),
    CONSTRAINT admin_email_otp_rate_limits_updated_order
        CHECK (updated_at >= window_started_at)
);

CREATE TABLE public.admin_email_otp_challenges (
    -- The browser holds the opaque selector; only its 32-byte digest is stored.
    challenge_hash bytea PRIMARY KEY,
    user_id bigint NOT NULL,
    -- Go computes this keyed HMAC before the repository boundary. A raw, or
    -- merely SHA-256-hashed, short numeric code must never enter PostgreSQL.
    code_mac bytea NOT NULL,
    attempt_count smallint NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz,
    CONSTRAINT admin_email_otp_challenges_user_id_foreign
        FOREIGN KEY (user_id)
        REFERENCES public.admin_users (id)
        ON DELETE CASCADE,
    CONSTRAINT admin_email_otp_challenges_challenge_hash_length
        CHECK (octet_length(challenge_hash) = 32),
    CONSTRAINT admin_email_otp_challenges_code_mac_length
        CHECK (octet_length(code_mac) = 32),
    CONSTRAINT admin_email_otp_challenges_attempt_count_supported
        CHECK (attempt_count BETWEEN 0 AND 5),
    CONSTRAINT admin_email_otp_challenges_expiry_order
        CHECK (expires_at > created_at),
    CONSTRAINT admin_email_otp_challenges_consumption_order
        CHECK (consumed_at IS NULL OR consumed_at >= created_at)
);

-- Creation consumes any prior outstanding row before inserting its replacement.
-- This unique partial index also rejects races that would leave two usable codes.
CREATE UNIQUE INDEX admin_email_otp_challenges_outstanding_user_idx
    ON public.admin_email_otp_challenges (user_id)
    WHERE consumed_at IS NULL;

CREATE INDEX admin_email_otp_challenges_expires_at_idx
    ON public.admin_email_otp_challenges (expires_at);

CREATE TABLE public.admin_oidc_identities (
    -- The verified pair, rather than mutable email, is the durable login key.
    issuer text NOT NULL,
    subject text NOT NULL,
    user_id bigint NOT NULL,
    created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_authenticated_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT admin_oidc_identities_pkey
        PRIMARY KEY (issuer, subject),
    CONSTRAINT admin_oidc_identities_user_id_foreign
        FOREIGN KEY (user_id)
        REFERENCES public.admin_users (id)
        ON DELETE CASCADE,
    -- One account may bind at most one identity from a given issuer.
    CONSTRAINT admin_oidc_identities_user_issuer_unique
        UNIQUE (user_id, issuer),
    CONSTRAINT admin_oidc_identities_issuer_trimmed
        CHECK (issuer = btrim(issuer)),
    CONSTRAINT admin_oidc_identities_issuer_length
        CHECK (char_length(issuer) BETWEEN 1 AND 255),
    CONSTRAINT admin_oidc_identities_subject_trimmed
        CHECK (subject = btrim(subject)),
    CONSTRAINT admin_oidc_identities_subject_length
        CHECK (char_length(subject) BETWEEN 1 AND 255),
    CONSTRAINT admin_oidc_identities_timestamp_order
        CHECK (last_authenticated_at >= created_at)
);
