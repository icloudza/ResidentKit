-- 设备自动接入：设备用自己的密钥申请，管理员在控制台同意后签发令牌
CREATE TABLE enrollments (
  id          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  device_uid  VARCHAR(64)  NOT NULL,
  name        VARCHAR(64)  NOT NULL,
  model       VARCHAR(32)  NOT NULL DEFAULT '',
  version     VARCHAR(32)  NOT NULL DEFAULT '',
  public_key  VARCHAR(128) NOT NULL COMMENT '设备公钥（base64，P-256 X9.63）',
  ip          VARCHAR(45)  NOT NULL,
  code        CHAR(6)      NOT NULL COMMENT '配对码（公钥指纹）',
  status      VARCHAR(16)  NOT NULL COMMENT 'pending | approved | completed | rejected | expired',
  token_id    BIGINT UNSIGNED NULL,
  decided_by  BIGINT UNSIGNED NULL,
  decided_at  DATETIME(3)  NULL,
  created_at  DATETIME(3)  NOT NULL,
  updated_at  DATETIME(3)  NOT NULL,
  PRIMARY KEY (id),
  KEY idx_enrollments_device (device_uid),
  KEY idx_enrollments_status (status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
