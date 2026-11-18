# 开放 API（v1）

外部系统用 API 密钥查询设备、调用设备命令、读取事件。**每个设备命令都是一个独立接口**，请求体就是命令参数，像 RPC 一样同步返回结果。

机器可读的规范：`GET /open/v1/openapi.json`（OpenAPI 3，按全部设备当前开放的命令动态生成，可直接导入 Postman 或生成客户端）。控制台「开放 API」页有完整文档与在线调试。

## 1. 接入

- **地址**：`https://<服务器>/open/v1`（与设备网关同一端口；服务器配置 `gateway.public_url` 后控制台显示完整地址）。
- **鉴权**：`Authorization: Bearer rkk_…`。密钥在控制台「开放 API → 密钥」创建（需再次输入密码），明文只显示一次。

| 密钥设置 | 说明 |
|---|---|
| 权限 | `devices:read` 查看设备与命令 · `commands:invoke` 调用命令并查询结果 · `events:read` 读取事件 |
| 设备范围 | 全部设备，或指定设备；范围外的设备一律 404 |
| 有效期 / 来源 IP | 可选 |

## 2. 接口

| 方法 | 路径 | 权限 | 说明 |
|---|---|---|---|
| GET | `/devices` | devices:read | 设备列表 |
| GET | `/devices/{id}` | devices:read | 设备详情 |
| GET | `/devices/{id}/commands` | devices:read | 这台设备开放的命令，含参数与返回字段 |
| GET | `/commands` | devices:read | 范围内全部设备的命令目录 |
| POST | `/devices/{id}/commands/{模块}/{命令}` | commands:invoke | **调用命令**（每个命令一个接口） |
| GET | `/invocations/{id}?wait=N` | commands:invoke | 查询异步或超时调用的结果 |
| GET | `/devices/{id}/events` | events:read | 事件分页 |

当前内置命令（由设备模块声明，随设备版本变化，以 `/devices/{id}/commands` 为准）：

| 接口 | 说明 | 参数 |
|---|---|---|
| `POST /devices/{id}/commands/example.heartbeat/status` | 心跳计数状态 | 无 |
| `POST /devices/{id}/commands/example.heartbeat/set` | 开关心跳计数 | `on`（bool，必填） |
| `POST /devices/{id}/commands/example.heartbeat/reset` | 计数清零 | 无 |
| `POST /devices/{id}/commands/net.link/status` | 服务器连接状态 | 无 |
| `POST /devices/{id}/commands/system.config/get` | 当前生效的配置 | 无 |
| `POST /devices/{id}/commands/system.update/status` | 版本与更新状态 | 无 |

配置下发、安装更新由系统内部调用，不对开放 API 开放。

## 3. 调用命令（RPC）

```http
POST /open/v1/devices/1/commands/example.heartbeat/set?timeout=10
Authorization: Bearer rkk_…
Idempotency-Key: job-20261117-001

{"on": true}
```

```json
{"code": 0, "msg": "ok", "data": {"invocation_id": 1024, "duration_ms": 142, "result": {"on": true, "beats": 0, "interval": 10}}}
```

- **默认同步**：等设备返回，最多 `timeout` 秒（1–30，默认 30）。
- **设备离线**：默认立即 409。带 `?ttl=秒` 则离线时排队，上线后执行；同步调用此时返回 202 `{invocation_id, status: "pending"}`。
- **异步**：`?async=true` 立即返回 202 `{invocation_id, status}`，之后 `GET /invocations/{id}?wait=30` 长轮询。
- **参数校验**：按设备声明校验必填、类型、范围，未知参数拒绝（400），不会下发到设备。设备未声明说明的命令原样下发。
- **幂等**：带 `Idempotency-Key`，同一密钥下相同的键只执行一次，重试返回同一结果（设备、命令或参数不同则 409）。

## 4. 错误

响应均为 `{code, msg, data}`，`code` 为 0 表示成功：

| HTTP | code | 含义 |
|---|---|---|
| 400 | 10001 | 参数错误 |
| 401 | 20001 | 缺少密钥，或密钥无效、已吊销、已过期 |
| 403 | 20006 | 没有该权限、IP 不在白名单、或命令由系统内部使用 |
| 404 | 10002 | 设备、命令或指令不存在（或不在密钥范围内） |
| 409 | 30001 | 设备离线 |
| 409 | 10004 | 幂等键已用于另一条指令（设备、命令或参数不同） |
| 202 | 0 | 带 ttl 且设备离线：已排队，上线后执行 |
| 422 | 30005 | 设备执行失败：msg 为设备返回的原因，`data.device_error_code` 为设备错误码 |
| 502 | 30006 | 结果未知：下发后连接断开或设备超时未返回 |
| 504 | 30003 | 设备未在 timeout 秒内返回：`data.invocation_id` 可用于之后查询 |

失败时 `data` 带 `invocation_id`（已创建指令时）。无效密钥会计入来源 IP 的失败次数。

## 5. 并发

- 接口无状态、不限流，可任意并发；建议复用连接（连接池）。
- 同一设备同时在途的命令最多 16 条，其余在服务器按提交顺序排队，有结果返回后依次下发，不会因突发量把设备挤掉线。
- 下发后超过命令的执行上限（默认 2 分钟，命令说明里的 `timeout` 可改，最长 1 小时）仍没有结果的指令记为 `lost` 并释放名额；之后结果到达仍会更新。

## 6. 为模块的命令写说明

在模块里实现 `+commandSchema`（见 `daemon/RKModule.h`），服务端据此生成接口、文档与参数校验：

```objc
+ (NSDictionary<NSString *, NSDictionary *> *)commandSchema {
    return @{ @"set": @{ @"label": @"开关计数", @"desc": @"开启或关闭心跳计数",
                         @"timeout": @10,   // 可选：执行上限（秒），默认 120
                         @"params": @[ @{ @"key": @"on", @"type": @"bool", @"label": @"启用计数", @"required": @YES } ],
                         @"result": @[ @{ @"key": @"beats", @"type": @"int", @"desc": @"已计次数" } ] } };
}
```
