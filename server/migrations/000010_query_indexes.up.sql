-- 让每条查询都走索引（internal/repository/explain_test.go 逐条 EXPLAIN 校验）。
ALTER TABLE invocations
  -- 按时间的统计、清理与开放 API 统计：覆盖索引，统计不回表
  ADD KEY idx_invocations_created (created_at, api_key_id, status, module, command),
  -- 设备排队 / 在途指令（隐含主键，按 id 排序不再 filesort）
  ADD KEY idx_invocations_device_status (device_id, status);

ALTER TABLE events
  DROP KEY idx_events_created,
  ADD KEY idx_events_created (created_at, type);

ALTER TABLE device_updates
  ADD KEY idx_device_updates_release (release_id, status),
  ADD KEY idx_device_updates_status (status);

-- 轮换后旧令牌都已吊销：按 (admin_id, revoked_at) 只命中仍有效的那几条
ALTER TABLE refresh_tokens
  DROP KEY idx_refresh_tokens_admin,
  ADD KEY idx_refresh_tokens_admin (admin_id, revoked_at),
  ADD KEY idx_refresh_tokens_expires (expires_at);

ALTER TABLE api_keys ADD KEY idx_api_keys_revoked (revoked_at);
