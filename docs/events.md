# 事件规范

事件是设备主动报告给服务器的消息（设备 → 服务器），用于记录「发生了什么」：状态变化、异常告警、业务数据。与之相对，远程指令是服务器 → 设备，心跳指标是周期性覆盖的「现在怎么样」。

## 1. 消息格式

```json
{
  "id": "6F1C…",
  "type": "net.network.changed",
  "level": "info",
  "ts": 1794830400.123,
  "payload": { "from": "wifi", "to": "cellular" }
}
```

| 字段 | 说明 |
|---|---|
| `id` | UUID，设备生成；服务端按它去重 |
| `type` | 事件类型，见第 2 节 |
| `level` | `info` · `notice` · `warning` · `error`，缺省为 `info` |
| `ts` | 设备上的发生时间，Unix 秒（可带小数） |
| `payload` | JSON 对象，见第 3 节 |

| 级别 | 含义 | 控制台 |
|---|---|---|
| `info` | 普通记录 | 只在事件流中显示 |
| `notice` | 值得留意，无需处理 | 蓝色标记 |
| `warning` | 可能需要关注 | 橙色标记，进入总览「最近告警」 |
| `error` | 出了问题 | 红色标记，进入总览「最近告警」 |

事件类型只使用中性色；红色、橙色只表示级别。

## 2. 类型命名

格式 `领域.对象.动作`：小写，点分段，段内用下划线连词，至少两段，总长 ≤ 64。正则：`^[a-z0-9_]+(\.[a-z0-9_]+)+$`。动作用过去式，常用 `started` · `stopped` · `changed` · `raised`（进入异常）· `cleared`（恢复正常）· `recovered` · `failed`。

| 前缀 | 用途 | 归属 |
|---|---|---|
| `system.` | daemon 自身 | 框架保留 |
| `net.` | 网络与服务器连接 | 框架保留 |
| `resource.` | 内存、CPU 等资源 | 框架保留 |
| `<模块 ID>.` | 模块自有事件，如 `example.heartbeat.reset` | 各模块 |

## 3. payload 规则

1. JSON 对象，序列化后 ≤ 4 KB。
2. 字段名 snake_case；带单位的加后缀：`_ms` · `_s` · `_bytes` · `_pct`。
3. 时间用 Unix 秒；枚举用小写英文字符串。
4. 不含任何敏感信息（token、密码、用户内容）。
5. 只能新增字段，不能删除或改变含义；不兼容的修改换新类型名（如加 `.v2`）。

## 4. 发送规则

- 至少送达一次：本地持久化队列，服务端确认（ack）后删除；服务端按 `id` 去重。同一设备内按发生顺序送达。
- 状态类事件只在变化时发送，不定时重复。
- 同类事件可设最短间隔（防抖），见第 5 节。
- 本地队列上限 500 条，超出丢弃最旧的。
- daemon 侧在发送前校验类型名、级别与 payload 大小，不合规的直接拒绝。

## 5. 内置事件（v1）

| 类型 | 级别 | 触发 | payload |
|---|---|---|---|
| `system.daemon.started` | `info`；`reason=respawn` 时 `warning` | daemon 每次启动 | `reason`: `boot`（重新越狱后首次）· `upgrade`（新版本首次）· `downgrade`（版本变低，如 OTA 自动回滚）· `restart`（正常退出后重启，如 launchctl 停止或重装同版本）· `respawn`（被杀或崩溃后重新拉起）；`last_exit`: `clean` · `watchdog` · `unknown`；`version`；`prev_version`（仅 upgrade / downgrade） |
| `system.service.toggled` | `notice` | 常驻服务总开关切换 | `enabled`；`by`: `app` · `remote` |
| `net.network.changed` | `info` | 网络类型变化，防抖 10 秒 | `from`、`to`: `wifi` · `cellular` · `wired` · `other` · `none` |
| `net.link.recovered` | `notice` | 与服务器断开 ≥ 30 秒后恢复 | `down_s`；`reason`；`reconnects` |
| `resource.memory.raised` | `warning`；`critical` 时 `error` | 系统内存压力升高，同类至少间隔 60 秒 | `level`、`prev`: `normal` · `warning` · `critical`；`dev_mem_pct` |
| `resource.memory.cleared` | `info` | 内存压力恢复正常 | `prev`；`duration_s` |
| `resource.footprint.high` | `warning` | daemon 内存超过 jetsam 上限的 75%，每小时最多一次 | `footprint_bytes`、`limit_bytes`、`pct` |

说明：

- 断开时发不出事件，所以只有 `net.link.recovered`，带上断开时长与原因；短于 30 秒的波动不发。
- 启动原因的判断：daemon 正常退出（SIGTERM）时写入「clean」标记，看门狗 abort 前写入「watchdog」标记；启动时读取上次标记，再对比版本号与 launchd 的启动时间（重新越狱是用户态重启，launchd 会重新启动）。
- `system.update.*` 见第 7 节。
- `resource.footprint.high` 用于在被 jetsam 杀掉之前发现泄漏（平时约 2 MB，上限 18 MB）。

## 6. 在模块中发送

```objc
[_ctx emitEvent:@"example.heartbeat.reset" level:RKEventInfo payload:@{ @"beats": @(n) }];
```

控制台在 `server/web/src/events.ts` 登记中文名称与说明；未登记的类型显示原始 payload。

## 7. OTA 更新事件

由 `system.update` 模块发送，服务端据此更新「发布」页的设备状态。

| 类型 | 级别 | 触发 | payload |
|---|---|---|---|
| `system.update.started` | `info` | 接受更新指令，开始下载 | `release_id`、`version`、`from` |
| `system.update.installed` | `notice` | 新版本安装完成并通过健康检查 | `release_id`、`version`、`from` |
| `system.update.rolled_back` | `warning` | 新版本安装失败或启动后不健康，已恢复上一版 | 同上，加 `error` |
| `system.update.failed` | `error` | 下载、校验或安装失败（未改动已安装版本），或无法回滚 | 同上，加 `stage`：`download` · `verify` · `install`（缺省为安装阶段） |

健康检查：安装后 90 秒内新 daemon 须可连通、版本正确，且 20 秒内没有重启。

## 8. 远程配置事件

由 `system.config` 模块发送，见 [config.md](config.md)。

| 类型 | 级别 | 触发 | payload |
|---|---|---|---|
| `system.config.applied` | 有改动 `notice`，无改动 `info` | 配置已生效 | `version`；`changed`：改动项 `模块.key` 列表 |
| `system.config.rejected` | `error` | 配置不合规，整份拒绝、保持原配置 | `version`、`error` |
