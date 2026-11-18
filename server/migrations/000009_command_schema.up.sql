-- 设备上报的远程命令说明（开放 API 为每个命令生成独立接口与文档）
ALTER TABLE devices ADD COLUMN command_schema JSON NULL COMMENT '{模块: {命令: 说明}}';
