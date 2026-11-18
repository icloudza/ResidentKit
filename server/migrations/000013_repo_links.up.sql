-- 软件源安装链接：/repo/<令牌>/ 供 Sileo 等添加为源，只提供已全量发布的最新版本。
-- 每个链接可设有效期、下载次数上限，可单独吊销；下载记录供控制台查看谁装过。
CREATE TABLE repo_links (
  id             BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  name           VARCHAR(64)  NOT NULL,
  token_hash     CHAR(64)     NOT NULL,
  token_prefix   VARCHAR(16)  NOT NULL,
  expires_at     DATETIME(3)  NULL,
  max_downloads  INT UNSIGNED NOT NULL DEFAULT 0 COMMENT '0 表示不限',
  downloads      INT UNSIGNED NOT NULL DEFAULT 0,
  last_used_at   DATETIME(3)  NULL,
  revoked_at     DATETIME(3)  NULL,
  created_by     BIGINT UNSIGNED NULL,
  created_at     DATETIME(3)  NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_repo_links_hash (token_hash)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE repo_downloads (
  id          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  link_id     BIGINT UNSIGNED NOT NULL,
  version     VARCHAR(32)  NOT NULL,
  ip          VARCHAR(45)  NOT NULL,
  machine     VARCHAR(32)  NOT NULL DEFAULT '' COMMENT '机型，取自 Sileo 的 X-Machine',
  firmware    VARCHAR(16)  NOT NULL DEFAULT '' COMMENT '系统版本，取自 X-Firmware',
  user_agent  VARCHAR(255) NOT NULL DEFAULT '',
  created_at  DATETIME(3)  NOT NULL,
  PRIMARY KEY (id),
  KEY idx_repo_downloads_link (link_id, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
