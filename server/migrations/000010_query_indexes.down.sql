ALTER TABLE invocations DROP KEY idx_invocations_created, DROP KEY idx_invocations_device_status;
ALTER TABLE events DROP KEY idx_events_created, ADD KEY idx_events_created (created_at);
ALTER TABLE device_updates DROP KEY idx_device_updates_release, DROP KEY idx_device_updates_status;
ALTER TABLE refresh_tokens DROP KEY idx_refresh_tokens_expires, DROP KEY idx_refresh_tokens_admin, ADD KEY idx_refresh_tokens_admin (admin_id);
ALTER TABLE api_keys DROP KEY idx_api_keys_revoked;
