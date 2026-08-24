package main

// The inquiry-retention advisory lock is a transaction-scoped database-wide
// reader/writer boundary shared by Contact creation and destructive retention.
// Its signed 64-bit value is the ASCII bytes "ZAFRETEN" and is intentionally
// distinct from the migration runner's "ZAFARMAN" session lock.
const inquiryRetentionAdvisoryLockID int64 = 0x5a4146524554454e

// A Contact transaction takes the shared form in its own statement. Under
// READ COMMITTED, the later INSERT therefore starts with a snapshot taken only
// after any already-running exclusive purge has committed its tombstone.
const acquireInquiryRetentionSharedLockSQL = `SELECT pg_catalog.pg_advisory_xact_lock_shared($1::bigint)`

// A maintenance transaction takes the exclusive form before it selects or
// deletes inquiry data. PostgreSQL releases either form when the owning
// transaction commits or rolls back; repository cancellation paths therefore
// rely on their deferred rollback.
const acquireInquiryRetentionExclusiveLockSQL = `SELECT pg_catalog.pg_advisory_xact_lock($1::bigint)`
