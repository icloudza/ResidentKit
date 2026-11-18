-- 接入码：控制台「添加设备」生成二维码，设备扫码后带着它申请接入，服务端核对后直接批准（不用在控制台点同意）。
CREATE TABLE enroll_tickets (
  id            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  name          VARCHAR(64)  NOT NULL,
  token_hash    CHAR(64)     NOT NULL,
  token_prefix  VARCHAR(16)  NOT NULL,
  expires_at    DATETIME(3)  NOT NULL,
  max_uses      INT UNSIGNED NOT NULL DEFAULT 1 COMMENT '0 表示不限',
  uses          INT UNSIGNED NOT NULL DEFAULT 0,
  last_used_at  DATETIME(3)  NULL,
  revoked_at    DATETIME(3)  NULL,
  created_by    BIGINT UNSIGNED NULL,
  created_at    DATETIME(3)  NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_enroll_tickets_hash (token_hash)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

ALTER TABLE enrollments ADD COLUMN ticket_id BIGINT UNSIGNED NULL COMMENT '经接入码直接批准时的接入码';
