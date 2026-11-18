-- 事件级别（docs/events.md）：info / notice / warning / error
ALTER TABLE events
  ADD COLUMN level VARCHAR(8) NOT NULL DEFAULT 'info' AFTER type,
  ADD KEY idx_events_level (level, id);
