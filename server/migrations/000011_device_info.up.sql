-- 设备信息：型号、系统版本、App 版本（hello 上报，net.link/info 刷新）
ALTER TABLE devices ADD COLUMN info JSON NULL COMMENT '{model, os, app_version}';
