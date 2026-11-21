<p align="center">
  <img src="design/AppIcon-readme.png" width="128" height="128" alt="ResidentKit">
</p>

<h1 align="center">ResidentKit</h1>

<p align="center">
  越狱 iOS 常驻守护进程、控制面板 App 与设备管理服务端
</p>

<p align="center">
  <a href="README.md">English</a> · 简体中文
</p>

<p align="center">
  <img src="https://img.shields.io/badge/iOS-14%2B-000000?logo=apple&logoColor=white" alt="iOS 14+">
  <img src="https://img.shields.io/badge/Dopamine-rootless-2E70ED" alt="Dopamine rootless">
  <img src="https://img.shields.io/badge/Objective--C-ARC-788799" alt="Objective-C">
  <img src="https://img.shields.io/badge/Go-Gin%20%2B%20MySQL-00ADD8?logo=go&logoColor=white" alt="Go">
  <img src="https://img.shields.io/badge/footprint-~2%20MB-2E70ED" alt="footprint ~2 MB">
</p>

---

业务逻辑运行在 launchd 托管的 daemon 中，App 仅作为 XPC 客户端。daemon 经 WebSocket 接入服务端，由 Web 控制台统一管理：远程指令、事件、远程配置、OTA，并提供开放 API。

- **环境**：Dopamine（rootless），已验证 iPhone SE 2 / iOS 18.7.1、iPhone 14 Pro / iOS 16.1.1；支持隐藏越狱。
- **注入**：模块如需向其他进程注入 dylib，设备须安装 ElleKit，并在 `control` 的 `Depends` 中声明 `mobilesubstrate`。若设备装有 Choicy 等注入管理器，需在其中放行目标进程的该 dylib，否则注入会被静默拦截。

## 组成

| 部分 | 目录 | 说明 |
|---|---|---|
| daemon | `daemon/` | `residentd`，模块化常驻进程；`rk-updater` 负责 OTA 安装与回滚 |
| App | `app/` | UIKit 控制面板，经 XPC 调用 daemon |
| 服务端 | `server/` | Go 单二进制，单端口 `8765` 提供设备网关（`/rk`）、控制台与开放 API（`/open/v1`） |

```
common/        XPC 协议、错误码、jetsam 私有接口
daemon/        RKModule.h 模块接口；Modules/ 内置模块；updater/ rk-updater
app/           Core/RKFeature.h 功能项接口；Features/ 功能项；UI/ 页面
layout/        LaunchDaemon plist、DEBIAN 脚本
scripts/       package-rootless.sh 打包
tools/rkctl.m  命令行调试客户端
server/        cmd/ rk-server、rk-release；internal/ 业务；migrations/ 表结构；web/ 控制台
```

## 功能

- **常驻**：launchd `KeepAlive` 退出即拉起；jetsam band 提升至 140；看门狗检测到 60 秒无响应时 `abort()` 交由 launchd 重启。
- **连接**：网络变化即时重连，心跳探活，前 5 次重连间隔不超过 3 秒；事件持久化，服务端确认后删除。
- **接入**：设备安装后自动申请，在控制台批准；或在「设备令牌 → 添加设备」生成二维码，App 扫码后直接接入。
- **远程指令**：离线排队、上线补发；每台设备并发上限 16。
- **远程配置**：按模块声明的参数校验后下发，即时生效。
- **OTA**：签名包按设备灰度推送；安装失败或启动后不健康自动回滚。
- **软件源**：控制台「发布」页生成安装链接，新设备在 Sileo 添加后安装最新全量版本。
- **开放 API**：每个设备命令对应一个接口，控制台提供文档与在线调试。

## 部署服务端

```bash
cd server
make package          # 前端 → Swagger → 测试 → 去符号静态二进制 → UPX，产物在 release/
```

1. 将 `rk-server` 与 `config.yaml`（由 `config.example.yaml` 复制）放在同一目录，填写数据库连接与 `auth` 下的两个密钥（`openssl rand -hex 32`）。
2. 直接运行 `rk-server`，无需参数；启动时自动建表、升级表结构。数据库要求 MySQL 5.7+（推荐 8.4）。
3. 反向代理负责域名与 HTTPS，转发到 `127.0.0.1:8765`（需支持 WebSocket，示例见 `server/deploy/`）。使用正规域名证书时无需证书指纹。
4. 首次访问控制台需输入初始化码（见服务日志）。建议开启两步验证。

升级时替换二进制并重启，设备自动重连。

| 地址 | 用途 |
|---|---|
| `wss://<域名>/rk` | 设备连接 |
| `https://<域名>` | 控制台 |
| `https://<域名>/open/v1` | 开放 API |
| `https://<域名>/repo/<令牌>/` | 软件源（`repo.enabled: true` 时启用） |

## 打包与安装

发布者本机需保存两个文件（不在仓库中，须备份）：

| 文件 | 用途 |
|---|---|
| `~/.config/residentkit/server.env` | `RK_SERVER_URL=wss://<域名>/rk`，打包时写入默认服务器；仅自签证书需填 `RK_SERVER_PIN` |
| `~/.config/residentkit/release-key.pem` | OTA 签名私钥，丢失后无法向已安装设备发布更新 |

签名工具 `rk-release` 用 `cd server && go build -o bin/rk-release ./cmd/rk-release` 构建。首次使用：`rk-release keygen` 生成私钥，将 `rk-release pubkey` 的输出填入服务端 `release.public_key`。

```bash
scripts/package-rootless.sh   # → packages/com.icloudza.residentkit_<版本>_iphoneos-arm64.deb
```

版本号取自 `control`，打包时同步写入 App 的 `Info.plist`。安装方式：Sileo 添加软件源，或传输 deb 后用 Sileo / Filza / `dpkg -i` 安装。

## 发布 OTA

1. 提高 `control` 中的 `Version`（须高于所有已发布版本），执行 `scripts/package-rootless.sh`。
2. 签名：`rk-release sign packages/<包名>.deb <版本>`，生成 `.rkrel.zip`。
3. 控制台「发布 → 发布新版本」上传；可选择同步到软件源。
4. 在版本详情中创建灰度计划，先推送单台设备，确认后推送全部。

设备端校验签名与 SHA-256 后由 `rk-updater` 安装（超时 10 分钟）。新版本须在 90 秒内连通且版本正确，否则回滚并上报 `system.update.rolled_back`。

## 开发模块

新增功能只需一个 daemon 模块文件；如需在 App 中展示，再增加一个功能项文件。构建按通配符收录，无需修改框架。

```objc
// daemon/Modules/MyModule.m
@interface MyModule : NSObject <RKModule> @end
@implementation MyModule { RKModuleContext *_ctx; }
RK_REGISTER_MODULE()
+ (NSString *)moduleIdentifier { return @"com.you.my"; }
- (instancetype)initWithContext:(RKModuleContext *)ctx { if ((self = [super init])) _ctx = ctx; return self; }
- (void)serviceDidStart {}
- (void)serviceWillStop {}
- (NSDictionary *)moduleStatus { return @{ @"on": [_ctx settingForKey:@"on"] ?: @NO }; }
- (void)handleCommand:(NSString *)cmd arguments:(NSDictionary *)args reply:(RKModuleReply)reply { … }

// 可选：允许服务端调用的命令（白名单）
+ (NSArray<NSString *> *)remoteCommands { return @[ @"set" ]; }
// 可选：命令说明，用于生成开放 API、参数校验与控制台表单
+ (NSDictionary<NSString *, NSDictionary *> *)commandSchema {
    return @{ @"set": @{ @"label": @"开关", @"icon": @"toggle", @"timeout": @10,
                         @"params": @[ @{ @"key": @"on", @"type": @"bool", @"required": @YES } ],
                         @"result": @[ @{ @"key": @"on", @"type": @"bool" } ] } };
}
// 可选：远程配置项，读取用 [_ctx config:@"interval_s"]
+ (NSArray<NSDictionary *> *)configSchema {
    return @[ @{ @"key": @"interval_s", @"type": @"int", @"default": @30, @"min": @10, @"max": @600 } ];
}
- (void)configDidChange:(NSDictionary *)changed {}
@end
```

- 回调均在串行队列 `context.queue` 上执行，模块内无需加锁。
- `-setSetting:forKey:` 原子落盘；状态变化后调用 `-postChange` 通知 App。
- 上报事件：`[_ctx emitEvent:@"com.you.my.done" level:@"notice" payload:@{…}]`。
- 参数类型：`bool`、`int`、`number`、`string`、`enum`、`object`、`array`（`configSchema` 不支持后两种）。

```objc
// app/Features/MyFeature.m
@interface MyFeature : RKFeature @end
@implementation MyFeature
RK_REGISTER_FEATURE()
- (RKFeatureStyle)style { return RKFeatureStyleRow; }
- (NSString *)title { return @"我的功能"; }
- (NSString *)symbolName { return @"bolt"; }
- (void)reload { [self.client invokeModule:@"com.you.my" command:@"status" arguments:nil completion:…]; }
@end
```

完整示例：`RKHeartbeatModule` + `RKHeartbeatFeature`。

## 开放 API

在控制台「开放 API」创建密钥，可限定权限、设备、来源 IP 与有效期。请求头 `Authorization: Bearer rkk_…`。

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/devices`、`/devices/{id}` | 设备列表与详情 |
| GET | `/devices/{id}/commands`、`/commands` | 可调用的命令及参数说明 |
| POST | `/devices/{id}/commands/{模块}/{命令}` | 调用命令，请求体为参数 |
| GET | `/invocations/{id}?wait=N` | 查询调用结果 |
| GET | `/devices/{id}/events` | 事件 |
| GET | `/openapi.json` | OpenAPI 3 规范 |

- 默认同步，`?timeout=` 1–30 秒；`?async=true` 返回 202。
- 设备离线返回 409；带 `?ttl=秒` 则排队等待上线。
- `Idempotency-Key` 保证同一键只执行一次。
- 响应格式 `{code, msg, data}`，`code` 为 0 表示成功；完整错误码见控制台文档。

## 协议

WebSocket 文本帧格式为 `{"id", "type", "payload", "ts"}`：设备连接后发送 `hello`（设备信息、命令与配置说明），双向 `ping`/`pong` 心跳，事件由服务端 `ack`，指令为 `invoke` / `result`。关闭码 `4401` 表示令牌失效，设备将重新申请接入。

事件级别：`info`、`notice`、`warning`、`error`。内置事件前缀：`system.daemon`、`system.update`、`system.config`、`net.link`、`resource`。

## 安全

- **传输**：`wss://`，域名证书按系统校验；自签证书使用证书固定。
- **调用白名单**：服务端只能调用 `remoteCommands` 中的命令；连接地址、令牌、安装更新不对开放 API 开放。
- **OTA**：ECDSA 签名，设备端校验，私钥仅存于发布者本机。
- **接入**：设备以 P-256 私钥签名申请，每台设备独立令牌，可单独吊销。
- **控制台**：argon2id 密码、JWT + 轮换刷新令牌、TOTP、失败锁定、敏感操作二次验证。
- **网关**：鉴权失败封禁 IP，拒绝浏览器来源连接，限制帧大小与连接数。

## 调试

```bash
rkctl ping | status | metrics
rkctl module <id> <command> [json]
rkctl mem <pid>
```

设备日志 subsystem 为 `com.icloudza.residentkit`（`idevicesyslog -p residentd`）。检查查询索引：`RK_EXPLAIN_DSN=… go test ./internal/repository -run TestExplain`。

## 性能

| 指标 | 结果 |
|---|---|
| daemon 内存 | 2–3 MB，空闲 CPU ≈ 0 |
| 被杀后恢复 | ≈ 0.25 s；连续被杀 ≈ 5.4 s（`ThrottleInterval=5`） |
| 卡死后恢复 | ≈ 60 s |
| 开放 API 并发 | 1000 并发调用，设备连接稳定 |

## 限制

- 半不完美越狱：设备重启后需重新越狱，daemon 随之自动加载。
- iOS 18 越狱目前仅支持 A12 / A13。
- 定制前缀时，Mach service 名（`RK_MACH_SERVICE`、plist `MachServices`、App entitlement）与 `RK_CLIENT_ENTITLEMENT` 须保持一致。
