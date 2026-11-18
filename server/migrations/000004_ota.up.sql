-- OTA：发布版本、灰度计划、每台设备的更新状态
CREATE TABLE releases (
  id          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  version     VARCHAR(32)  NOT NULL,
  package     VARCHAR(64)  NOT NULL,
  size        BIGINT       NOT NULL,
  sha256      CHAR(64)     NOT NULL,
  signature   VARCHAR(255) NOT NULL COMMENT 'ECDSA P-256，base64',
  notes       TEXT         NULL COMMENT '更新说明',
  file_name   VARCHAR(255) NOT NULL,
  status      VARCHAR(16)  NOT NULL COMMENT 'published | withdrawn',
  created_by  BIGINT UNSIGNED NULL,
  created_at  DATETIME(3)  NOT NULL,
  updated_at  DATETIME(3)  NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_releases_version (version)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE rollouts (
  id          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  release_id  BIGINT UNSIGNED NOT NULL,
  target      VARCHAR(16)  NOT NULL COMMENT 'all | devices',
  device_ids  JSON         NULL,
  status      VARCHAR(16)  NOT NULL COMMENT 'active | paused | cancelled | completed',
  created_by  BIGINT UNSIGNED NULL,
  created_at  DATETIME(3)  NOT NULL,
  updated_at  DATETIME(3)  NOT NULL,
  PRIMARY KEY (id),
  KEY idx_rollouts_release (release_id),
  KEY idx_rollouts_status (status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE device_updates (
  id             BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  device_id      BIGINT UNSIGNED NOT NULL,
  release_id     BIGINT UNSIGNED NOT NULL,
  rollout_id     BIGINT UNSIGNED NOT NULL,
  status         VARCHAR(16)  NOT NULL COMMENT 'queued | downloading | installed | failed | rolled_back | unsupported',
  error          VARCHAR(255) NOT NULL DEFAULT '',
  invocation_id  BIGINT UNSIGNED NULL,
  created_at     DATETIME(3)  NOT NULL,
  updated_at     DATETIME(3)  NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_device_updates (device_id, release_id),
  KEY idx_device_updates_rollout (rollout_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
