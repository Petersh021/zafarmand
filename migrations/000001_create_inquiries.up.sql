-- Stage 13 creates only the storage boundary already understood from the
-- Contact form. The public handler does not insert into this table yet.
CREATE TABLE public.inquiries (
    -- PostgreSQL supplies an internal monotonic key; no public UUID is needed.
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    -- Visitor values stay as text while named checks enforce storage limits.
    name text NOT NULL,
    email text NOT NULL,
    discipline text NOT NULL,
    message text NOT NULL,
    -- Text plus a named check is easier to extend than a PostgreSQL enum.
    status text NOT NULL DEFAULT 'new',
    -- Time-zone-aware timestamps retain one unambiguous event timeline.
    created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    -- Go performs richer validation; these checks protect stored invariants if
    -- another client writes directly to PostgreSQL in the future.
    CONSTRAINT inquiries_name_trimmed
        CHECK (name = btrim(name)),
    CONSTRAINT inquiries_name_length
        CHECK (char_length(name) BETWEEN 1 AND 100),
    CONSTRAINT inquiries_email_trimmed
        CHECK (email = btrim(email)),
    CONSTRAINT inquiries_email_length
        CHECK (char_length(email) BETWEEN 3 AND 254),
    CONSTRAINT inquiries_discipline_supported
        CHECK (
            discipline IN (
                'interior-design',
                'architecture-design',
                'products'
            )
        ),
    CONSTRAINT inquiries_message_trimmed
        CHECK (message = btrim(message)),
    CONSTRAINT inquiries_message_length
        CHECK (char_length(message) BETWEEN 1 AND 3000),
    CONSTRAINT inquiries_status_supported
        CHECK (status IN ('new', 'reviewed', 'archived')),
    -- Future updates must never claim to predate the original inquiry.
    CONSTRAINT inquiries_timestamp_order
        CHECK (updated_at >= created_at)
);
