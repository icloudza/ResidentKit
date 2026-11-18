-- 开放 API 调用记录：来源 IP；按密钥与时间查询的索引（统计与调用记录页）
ALTER TABLE invocations
  ADD COLUMN source_ip VARCHAR(45) NOT NULL DEFAULT '' COMMENT '发起请求的 IP（控制台或开放 API）',
  ADD KEY idx_invocations_api_time (api_key_id, created_at);
