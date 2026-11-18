-- 开放 API：API 密钥；指令记录发起的密钥与幂等键（同一密钥同一幂等键只执行一次）
CREATE TABLE api_keys (
  id            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  name          VARCHAR(64)  NOT NULL,
  key_hash      CHAR(64)     NOT NULL,
  key_prefix    VARCHAR(16)  NOT NULL,
  scopes        JSON         NOT NULL COMMENT '["devices:read","commands:invoke","events:read"]',
  device_ids    JSON         NULL     COMMENT '为空表示全部设备',
  allowed_ips   JSON         NULL     COMMENT '为空表示不限',
  expires_at    DATETIME(3)  NULL,
  created_by    BIGINT UNSIGNED NULL,
  last_used_at  DATETIME(3)  NULL,
  last_used_ip  VARCHAR(45)  NOT NULL DEFAULT '',
  revoked_at    DATETIME(3)  NULL,
  created_at    DATETIME(3)  NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_api_keys_hash (key_hash)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

ALTER TABLE invocations
  ADD COLUMN api_key_id BIGINT UNSIGNED NULL COMMENT '经开放 API 发起时的密钥',
  ADD COLUMN idempotency_key VARCHAR(64) NULL,
  ADD UNIQUE KEY uk_invocations_idem (api_key_id, idempotency_key);
