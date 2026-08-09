-- Stage 15 creates the smallest durable authentication boundary needed by the
-- future owner/editor admin area. Authentication behavior remains in Go; these
-- tables protect user identities, password verifiers, and revocable sessions.
CREATE TABLE public.admin_users (
    -- PostgreSQL owns the internal identity, which is never used as a secret.
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    -- Application normalization lowercases and trims email before storage. The
    -- named checks below also defend that invariant for direct database writes.
    email text NOT NULL,
    -- Store only a one-way password hash produced by the application, never a
    -- plaintext password. The bound accommodates established encoded formats.
    password_hash text NOT NULL,
    -- Text plus a named check keeps the initial authorization model explicit
    -- while remaining straightforward to extend in a future migration.
    role text NOT NULL,
    -- Deactivation blocks access without deleting the administrator's identity.
    active boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT admin_users_email_normalized
        CHECK (email = lower(btrim(email))),
    CONSTRAINT admin_users_email_length
        CHECK (char_length(email) BETWEEN 3 AND 254),
    CONSTRAINT admin_users_email_unique
        UNIQUE (email),
    CONSTRAINT admin_users_password_hash_trimmed
        CHECK (password_hash = btrim(password_hash)),
    CONSTRAINT admin_users_password_hash_length
        CHECK (char_length(password_hash) BETWEEN 1 AND 255),
    CONSTRAINT admin_users_role_supported
        CHECK (role IN ('owner', 'editor')),
    CONSTRAINT admin_users_timestamp_order
        CHECK (updated_at >= created_at)
);

-- Session rows contain only digests of browser-held secrets. Deleting a row or
-- setting revoked_at invalidates that session without revealing either token.
CREATE TABLE public.admin_sessions (
    -- The token digest is both the lookup key and the table identity; storing
    -- the original bearer token would turn a database leak into active access.
    token_hash bytea PRIMARY KEY,
    user_id bigint NOT NULL,
    -- The CSRF secret receives the same one-way treatment as the bearer token.
    csrf_token_hash bytea NOT NULL,
    created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    CONSTRAINT admin_sessions_user_id_foreign
        FOREIGN KEY (user_id)
        REFERENCES public.admin_users (id)
        ON DELETE CASCADE,
    CONSTRAINT admin_sessions_token_hash_length
        CHECK (octet_length(token_hash) = 32),
    CONSTRAINT admin_sessions_csrf_token_hash_length
        CHECK (octet_length(csrf_token_hash) = 32),
    CONSTRAINT admin_sessions_expiry_order
        CHECK (expires_at > created_at),
    CONSTRAINT admin_sessions_revocation_order
        CHECK (revoked_at IS NULL OR revoked_at >= created_at)
);

-- PostgreSQL indexes the session primary key automatically. These two explicit
-- indexes cover user-wide revocation/deletion and chronological expiry cleanup.
CREATE INDEX admin_sessions_user_id_idx
    ON public.admin_sessions (user_id);

CREATE INDEX admin_sessions_expires_at_idx
    ON public.admin_sessions (expires_at);
