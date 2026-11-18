ALTER TABLE invocations DROP INDEX uk_invocations_idem, DROP COLUMN idempotency_key, DROP COLUMN api_key_id;
DROP TABLE IF EXISTS api_keys;
