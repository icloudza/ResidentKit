-- 设备活动：按设备与时间统计 / 筛选（覆盖索引，统计不回表）
ALTER TABLE invocations ADD KEY idx_invocations_device_time (device_id, created_at, status);
ALTER TABLE events ADD KEY idx_events_device_time (device_id, created_at, level);
