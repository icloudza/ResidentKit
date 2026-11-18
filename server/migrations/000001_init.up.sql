-- ResidentKit 表结构 v1（MySQL 5.7+/8.0，InnoDB，utf8mb4）

CREATE TABLE devices (
  id               BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  device_uid       VARCHAR(64)  NOT NULL COMMENT '设备在 hello 中上报的 ID',
  name             VARCHAR(64)  NOT NULL DEFAULT '',
  alias            VARCHAR(64)  NOT NULL DEFAULT '' COMMENT '控制台设置的别名',
  version          VARCHAR(32)  NOT NULL DEFAULT '',
  online           TINYINT(1)   NOT NULL DEFAULT 0,
  last_ip          VARCHAR(45)  NOT NULL DEFAULT '',
  network          VARCHAR(16)  NOT NULL DEFAULT '',
  rtt_ms           INT          NOT NULL DEFAULT 0,
  remote_commands  JSON         NULL COMMENT '{模块: [命令, ...]}',
  first_seen_at    DATETIME(3)  NOT NULL,
  last_seen_at     DATETIME(3)  NOT NULL,
  created_at       DATETIME(3)  NOT NULL,
  updated_at       DATETIME(3)  NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_devices_uid (device_uid),
  KEY idx_devices_online (online, last_seen_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE device_tokens (
  id            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  name          VARCHAR(64)  NOT NULL,
  token_hash    CHAR(64)     NOT NULL COMMENT 'SHA-256，不存明文',
  token_prefix  VARCHAR(16)  NOT NULL COMMENT '前缀，仅用于展示',
  device_id     BIGINT UNSIGNED NULL COMMENT '首次 hello 时绑定',
  created_by    BIGINT UNSIGNED NULL,
  last_used_at  DATETIME(3)  NULL,
  revoked_at    DATETIME(3)  NULL,
  created_at    DATETIME(3)  NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_device_tokens_hash (token_hash),
  KEY idx_device_tokens_device (device_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE events (
  id          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  device_id   BIGINT UNSIGNED NOT NULL,
  msg_id      VARCHAR(64)  NOT NULL,
  type        VARCHAR(64)  NOT NULL,
  payload     JSON         NULL,
  device_ts   DATETIME(3)  NULL,
  created_at  DATETIME(3)  NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_events_device_msg (device_id, msg_id) COMMENT '至少一次投递的去重',
  KEY idx_events_device (device_id, id),
  KEY idx_events_type (type, id),
  KEY idx_events_created (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE invocations (
  id             BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  uid            CHAR(32)     NOT NULL COMMENT '协议中的消息 ID',
  device_id      BIGINT UNSIGNED NOT NULL,
  module         VARCHAR(64)  NOT NULL,
  command        VARCHAR(64)  NOT NULL,
  arguments      JSON         NULL,
  status         VARCHAR(16)  NOT NULL COMMENT 'pending|sent|succeeded|failed|expired|lost',
  result         JSON         NULL,
  error_code     INT          NOT NULL DEFAULT 0,
  error_message  VARCHAR(255) NOT NULL DEFAULT '',
  operator_id    BIGINT UNSIGNED NULL,
  expires_at     DATETIME(3)  NOT NULL,
  sent_at        DATETIME(3)  NULL,
  finished_at    DATETIME(3)  NULL,
  created_at     DATETIME(3)  NOT NULL,
  updated_at     DATETIME(3)  NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_invocations_uid (uid),
  KEY idx_invocations_device (device_id, id),
  KEY idx_invocations_status (status, expires_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE admins (
  id             BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  username       VARCHAR(32)  NOT NULL,
  password_hash  VARCHAR(255) NOT NULL COMMENT 'argon2id',
  role           VARCHAR(16)  NOT NULL DEFAULT 'admin',
  totp_secret    VARCHAR(255) NOT NULL DEFAULT '' COMMENT 'AES-GCM 加密',
  totp_enabled   TINYINT(1)   NOT NULL DEFAULT 0,
  failed_count   INT          NOT NULL DEFAULT 0,
  locked_until   DATETIME(3)  NULL,
  last_login_at  DATETIME(3)  NULL,
  last_login_ip  VARCHAR(45)  NOT NULL DEFAULT '',
  created_at     DATETIME(3)  NOT NULL,
  updated_at     DATETIME(3)  NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_admins_username (username)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE refresh_tokens (
  id          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  admin_id    BIGINT UNSIGNED NOT NULL,
  token_hash  CHAR(64)     NOT NULL,
  user_agent  VARCHAR(255) NOT NULL DEFAULT '',
  ip          VARCHAR(45)  NOT NULL DEFAULT '',
  expires_at  DATETIME(3)  NOT NULL,
  revoked_at  DATETIME(3)  NULL,
  created_at  DATETIME(3)  NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_refresh_tokens_hash (token_hash),
  KEY idx_refresh_tokens_admin (admin_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- 只追加：运行账号对此表仅有 INSERT/SELECT 权限。
CREATE TABLE audit_logs (
  id          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  admin_id    BIGINT UNSIGNED NULL,
  username    VARCHAR(32)  NOT NULL DEFAULT '',
  action      VARCHAR(48)  NOT NULL,
  target      VARCHAR(128) NOT NULL DEFAULT '',
  ip          VARCHAR(45)  NOT NULL DEFAULT '',
  success     TINYINT(1)   NOT NULL DEFAULT 1,
  detail      JSON         NULL,
  created_at  DATETIME(3)  NOT NULL,
  PRIMARY KEY (id),
  KEY idx_audit_created (created_at),
  KEY idx_audit_admin (admin_id, id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
