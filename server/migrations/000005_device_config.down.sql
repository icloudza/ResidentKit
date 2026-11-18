DROP TABLE IF EXISTS device_configs;
ALTER TABLE devices DROP COLUMN config_version, DROP COLUMN config_schema;
