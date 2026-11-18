# 远程配置

在控制台修改设备的运行参数（采样间隔、心跳间隔、阈值等），保存后立即下发、无需重启；离线设备上线后自动补发。

只放「调参」类配置。连接地址、令牌、证书固定等仍是本机设置，不能远程修改——服务端即使被攻破，也不能借配置把设备引向别处。

## 1. 在模块中声明

```objc
+ (NSArray<NSDictionary *> *)configSchema {
    return @[ @{ @"key": @"interval_s", @"type": @"int", @"default": @30, @"min": @10, @"max": @600,
                 @"label": @"采样间隔（秒）", @"desc": @"检查内存压力的频率" } ];
}

- (void)configDidChange:(NSDictionary *)changed {   // 改动的 key → 新的生效值
    if (changed[@"interval_s"]) [self reschedule];
}

// 读取：[_ctx config:@"interval_s"]，未下发时返回默认值
```

| 字段 | 说明 |
|---|---|
| `key` | snake_case，带单位的加后缀（`_s`、`_pct`）；同一模块内唯一 |
| `type` | `bool` · `int` · `number` · `string` · `enum` |
| `default` | 必填，须满足下面的约束，否则该项被忽略 |
| `label` / `desc` | 控制台显示的中文名称与说明 |
| `min` / `max` | 数值范围（`int`、`number`） |
| `options` | 可选值（`enum`） |
| `max_len` | 长度上限（`string`，默认 256） |

控制台按声明自动生成表单，不需要改前端；模块中文名在 `server/web/src/format.ts` 的 `moduleName` 登记。

## 2. 同步流程

1. 设备在 hello 中上报 `config_schema`（全部声明）与 `config_version`（已生效版本）。
2. 管理员保存配置：服务端校验后版本号 +1，保存，在线设备立即收到 `system.config/apply`。
3. 设备整体替换配置：先全部校验，任一项不合规则整份拒绝、保持原配置；通过后持久化并回调各模块的 `-configDidChange:`。
4. 设备发事件 `system.config.applied {version, changed}` 或 `system.config.rejected {version, error}`，服务端据此显示「已生效 / 下发中 / 设备拒绝」。
5. 设备上线时，已生效版本与服务端不一致就补发；已被拒绝的版本不重发，等管理员修改。

下发的是完整快照：未出现的项恢复默认，与默认值相同的项不保存。版本号只增不减。

## 3. 内置配置项

| 模块 | key | 默认 | 范围 | 说明 |
|---|---|---|---|---|
| `net.link` | `heartbeat_s` | 25 | 10–120 | 心跳与指标上报间隔；超过 2.8 个心跳无回应视为断开 |
| `system.monitor` | `interval_s` | 30 | 10–600 | 内存压力与服务内存的采样间隔 |
| `system.monitor` | `footprint_pct` | 75 | 50–95 | 服务内存告警线（占 jetsam 上限） |
| `example.heartbeat` | `interval_s` | 10 | 2–3600 | 示例模块的计数间隔 |
