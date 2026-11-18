-- 远程配置：设备上报的配置声明与已生效版本，服务端保存的期望配置
ALTER TABLE devices
  ADD COLUMN config_schema JSON NULL COMMENT '设备上报的可配置项 {模块: [配置项]}',
  ADD COLUMN config_version BIGINT NOT NULL DEFAULT 0 COMMENT '设备已生效的配置版本';

CREATE TABLE device_configs (
  device_id   BIGINT UNSIGNED NOT NULL,
  version     BIGINT       NOT NULL,
  config      JSON         NOT NULL COMMENT '{模块: {key: 值}}，只含与默认值不同的项',
  error       VARCHAR(255) NOT NULL DEFAULT '' COMMENT '设备最近一次拒绝的原因',
  updated_by  BIGINT UNSIGNED NULL,
  updated_at  DATETIME(3)  NOT NULL,
  PRIMARY KEY (device_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
