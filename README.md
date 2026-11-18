<p align="center">
  <img src="design/AppIcon-readme.png" width="128" height="128" alt="ResidentKit">
</p>

<h1 align="center">ResidentKit</h1>

<p align="center">
  Resident daemon, control app and device management server for jailbroken iOS
</p>

<p align="center">
  English · <a href="README.zh-CN.md">简体中文</a>
</p>

<p align="center">
  <img src="https://img.shields.io/badge/iOS-14%2B-000000?logo=apple&logoColor=white" alt="iOS 14+">
  <img src="https://img.shields.io/badge/Dopamine-rootless-2E70ED" alt="Dopamine rootless">
  <img src="https://img.shields.io/badge/Objective--C-ARC-788799" alt="Objective-C">
  <img src="https://img.shields.io/badge/Go-Gin%20%2B%20MySQL-00ADD8?logo=go&logoColor=white" alt="Go">
  <img src="https://img.shields.io/badge/footprint-~2%20MB-2E70ED" alt="footprint ~2 MB">
</p>

---

Business logic runs in a launchd-managed daemon; the app is only an XPC client. The daemon connects to the server over WebSocket, and a web console manages the fleet: remote commands, events, remote configuration, OTA updates, and an open API.

- **Environment**: Dopamine (rootless), verified on iPhone SE 2 / iOS 18.7.1 and iPhone 14 Pro / iOS 16.1.1. Hidden-jailbreak mode is supported.
- **Injection**: modules that inject a dylib into other processes require ElleKit on the device; declare `mobilesubstrate` in the `Depends` field of `control`.

## Components

| Part | Directory | Description |
|---|---|---|
| daemon | `daemon/` | `residentd`, a modular resident process; `rk-updater` installs OTA updates and rolls back |
| App | `app/` | UIKit control panel that calls the daemon over XPC |
| Server | `server/` | Single Go binary on one port (`8765`): device gateway (`/rk`), console, and open API (`/open/v1`) |

```
common/        XPC protocol, error codes, private jetsam interfaces
daemon/        RKModule.h module interface; Modules/ built-in modules; updater/ rk-updater
app/           Core/RKFeature.h feature interface; Features/ features; UI/ screens
layout/        LaunchDaemon plist, DEBIAN scripts
scripts/       package-rootless.sh packaging
tools/rkctl.m  command-line debug client
server/        cmd/ rk-server, rk-release; internal/ application; migrations/ schema; web/ console
```

## Features

- **Residency**: launchd `KeepAlive` restarts the daemon on any exit; jetsam band raised to 140; a watchdog calls `abort()` after 60 s without response so launchd restarts it.
- **Connectivity**: immediate reconnect on network changes, heartbeat probing, first 5 retries within 3 s; events are persisted until acknowledged by the server.
- **Enrollment**: the device requests access after installation and an administrator approves it, or the console generates a QR code (Device Tokens → Add Device) that the app scans to enroll directly.
- **Remote commands**: queued while offline and delivered on reconnect; up to 16 in flight per device.
- **Remote configuration**: validated against the module's declared schema and applied immediately.
- **OTA**: signed packages rolled out to selected devices; automatic rollback when installation fails or the new version is unhealthy.
- **Package repository**: the Releases page issues install links that Sileo can add as a source to install the latest released version.
- **Open API**: one endpoint per device command, with documentation and a request console built into the web UI.

## Server Deployment

```bash
cd server
make package          # frontend → Swagger → tests → stripped static binary → UPX, output in release/
```

1. Place `rk-server` and `config.yaml` (copied from `config.example.yaml`) in the same directory. Fill in the database connection and the two `auth` keys (`openssl rand -hex 32`).
2. Run `rk-server` without arguments. The schema is created and migrated on startup. Requires MySQL 5.7+ (8.4 recommended).
3. Terminate HTTPS at a reverse proxy and forward to `127.0.0.1:8765` with WebSocket support (see `server/deploy/`). Certificates from a public CA need no pinning.
4. Open the console and enter the setup code printed in the server log. Enabling two-factor authentication is recommended.

To upgrade, replace the binary and restart; devices reconnect automatically.

| URL | Purpose |
|---|---|
| `wss://<domain>/rk` | Device connection |
| `https://<domain>` | Console |
| `https://<domain>/open/v1` | Open API |
| `https://<domain>/repo/<token>/` | Package repository (when `repo.enabled: true`) |

## Packaging and Installation

Two files on the publisher's machine, kept outside the repository (back them up):

| File | Purpose |
|---|---|
| `~/.config/residentkit/server.env` | `RK_SERVER_URL=wss://<domain>/rk`, the default server embedded at build time; `RK_SERVER_PIN` only for self-signed certificates |
| `~/.config/residentkit/release-key.pem` | OTA signing key; if lost, installed devices can no longer receive updates |

Build the signing tool with `cd server && go build -o bin/rk-release ./cmd/rk-release`. On first use, run `rk-release keygen` and put the output of `rk-release pubkey` into the server's `release.public_key`.

```bash
scripts/package-rootless.sh   # → packages/com.icloudza.residentkit_<version>_iphoneos-arm64.deb
```

The version is taken from `control` and written into the app's `Info.plist` at build time. Install by adding the repository in Sileo, or copy the deb to the device and install it with Sileo, Filza, or `dpkg -i`.

## Publishing OTA Updates

1. Bump `Version` in `control` above every published version, then run `scripts/package-rootless.sh`.
2. Sign: `rk-release sign packages/<package>.deb <version>` produces a `.rkrel.zip`.
3. Upload it on the console's Releases page, optionally syncing it to the package repository.
4. Create a rollout in the release details: one device first, then all devices.

The device verifies the signature and SHA-256, then `rk-updater` installs the package (10-minute timeout). The new version must connect with the expected version within 90 s; otherwise it is rolled back and `system.update.rolled_back` is reported.

## Writing a Module

A new capability needs one daemon module file, plus one feature file if it should appear in the app. Sources are collected by wildcard; no framework changes are required.

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

// Optional: commands the server may invoke (allowlist)
+ (NSArray<NSString *> *)remoteCommands { return @[ @"set" ]; }
// Optional: command schema for the open API, parameter validation and console forms
+ (NSDictionary<NSString *, NSDictionary *> *)commandSchema {
    return @{ @"set": @{ @"label": @"Toggle", @"icon": @"toggle", @"timeout": @10,
                         @"params": @[ @{ @"key": @"on", @"type": @"bool", @"required": @YES } ],
                         @"result": @[ @{ @"key": @"on", @"type": @"bool" } ] } };
}
// Optional: remote configuration, read with [_ctx config:@"interval_s"]
+ (NSArray<NSDictionary *> *)configSchema {
    return @[ @{ @"key": @"interval_s", @"type": @"int", @"default": @30, @"min": @10, @"max": @600 } ];
}
- (void)configDidChange:(NSDictionary *)changed {}
@end
```

- All callbacks run on the serial queue `context.queue`; modules need no locking.
- `-setSetting:forKey:` persists atomically; call `-postChange` to notify the app of state changes.
- Emit events with `[_ctx emitEvent:@"com.you.my.done" level:@"notice" payload:@{…}]`.
- Parameter types: `bool`, `int`, `number`, `string`, `enum`, `object`, `array` (the last two are not available in `configSchema`).

```objc
// app/Features/MyFeature.m
@interface MyFeature : RKFeature @end
@implementation MyFeature
RK_REGISTER_FEATURE()
- (RKFeatureStyle)style { return RKFeatureStyleRow; }
- (NSString *)title { return @"My Feature"; }
- (NSString *)symbolName { return @"bolt"; }
- (void)reload { [self.client invokeModule:@"com.you.my" command:@"status" arguments:nil completion:…]; }
@end
```

Complete example: `RKHeartbeatModule` + `RKHeartbeatFeature`.

## Open API

Create keys on the console's Open API page; each key can be limited by scope, devices, source IP, and expiry. Authenticate with `Authorization: Bearer rkk_…`.

| Method | Path | Description |
|---|---|---|
| GET | `/devices`, `/devices/{id}` | Devices |
| GET | `/devices/{id}/commands`, `/commands` | Available commands and their schemas |
| POST | `/devices/{id}/commands/{module}/{command}` | Invoke a command; the body holds the arguments |
| GET | `/invocations/{id}?wait=N` | Invocation result |
| GET | `/devices/{id}/events` | Events |
| GET | `/openapi.json` | OpenAPI 3 specification |

- Calls are synchronous by default (`?timeout=` 1–30 s); `?async=true` returns 202.
- Offline devices return 409; with `?ttl=<seconds>` the call is queued until the device reconnects.
- `Idempotency-Key` ensures a key is executed only once.
- Responses use `{code, msg, data}`, where `code` 0 means success. Error codes are documented in the console.

## Protocol

Each WebSocket text frame is `{"id", "type", "payload", "ts"}`. After connecting, the device sends `hello` (device info, command and configuration schemas); `ping`/`pong` serve as heartbeats; events are acknowledged with `ack`; commands use `invoke` / `result`. Close code `4401` means the token is no longer valid and the device re-enrolls.

Event levels: `info`, `notice`, `warning`, `error`. Built-in event prefixes: `system.daemon`, `system.update`, `system.config`, `net.link`, `resource`.

## Security

- **Transport**: `wss://` with system certificate validation; certificate pinning for self-signed certificates.
- **Command allowlist**: the server can invoke only commands listed in `remoteCommands`; connection settings, tokens, and update installation are not exposed through the open API.
- **OTA**: ECDSA-signed packages verified on the device; the private key stays on the publisher's machine.
- **Enrollment**: devices sign requests with their own P-256 key; each device has its own revocable token.
- **Console**: argon2id passwords, JWT with rotating refresh tokens, TOTP, lockout after failed logins, re-authentication for sensitive actions.
- **Gateway**: IP bans after repeated authentication failures, browser-origin connections rejected, frame size and connection limits.

## Debugging

```bash
rkctl ping | status | metrics
rkctl module <id> <command> [json]
rkctl mem <pid>
```

Device logs use the subsystem `com.icloudza.residentkit` (`idevicesyslog -p residentd`). Check query index usage with `RK_EXPLAIN_DSN=… go test ./internal/repository -run TestExplain`.

## Performance

| Metric | Result |
|---|---|
| Daemon memory | 2–3 MB, idle CPU ≈ 0 |
| Recovery after kill | ≈ 0.25 s; ≈ 5.4 s under repeated kills (`ThrottleInterval=5`) |
| Recovery after hang | ≈ 60 s |
| Open API concurrency | 1000 concurrent calls with stable device connections |

## Limitations

- Semi-untethered jailbreak: after a reboot the device must be re-jailbroken; the daemon then loads automatically.
- iOS 18 jailbreaks currently support A12 / A13 devices only.
- When changing the identifier prefix, keep the Mach service name (`RK_MACH_SERVICE`, plist `MachServices`, app entitlement) and `RK_CLIENT_ENTITLEMENT` consistent.
