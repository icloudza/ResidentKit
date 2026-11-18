-- 设备在线时长与心跳上报的资源指标
ALTER TABLE devices
  ADD COLUMN connected_at DATETIME(3) NULL COMMENT '本次上线时间' AFTER online,
  ADD COLUMN metrics      JSON        NULL COMMENT '最近一次心跳上报的内存 / CPU' AFTER remote_commands,
  ADD COLUMN metrics_at   DATETIME(3) NULL AFTER metrics;
