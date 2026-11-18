// RKModule.h — residentd 的扩展点。
//
// 模块是一块独立的常驻工作（定时器、文件监听、网络监听……）。在 daemon/Modules/
// 下加文件、遵循 RKModule、在 @implementation 里写 RK_REGISTER_MODULE() 即可：
// 构建按通配符收录，host 启动时实例化，daemon 其他地方无需改动。
//
// 约定
//   * 线程：所有回调（init、start/stop、命令）都在 context.queue 上执行，这是
//     所有模块共享的串行队列。自己的 timer / source 也挂在上面，就不需要锁。
//   * 生命周期：总开关打开时（启动时及每次打开）调 -serviceDidStart，关闭时调
//     -serviceWillStop，只在两者之间干活。进程随时可能被杀，需要保留的数据走
//     -setSetting:forKey:。
//   * 命令：app 经 RKClient 的 -invokeModule:... 调到 -handleCommand:arguments:reply:。
//     "status" 为内置命令，返回 -moduleStatus。参数/结果必须是 property list。
//   * 预算：整个 daemon 空闲约 1.6 MB，jetsam 上限 18 MB。缓冲区要小，及时释放
//     autorelease 对象（队列已按 block 自动 drain）。

#import <Foundation/Foundation.h>
#import <os/log.h>

NS_ASSUME_NONNULL_BEGIN

// 事件级别（docs/events.md）。
static NSString *const RKEventInfo    = @"info";
static NSString *const RKEventNotice  = @"notice";
static NSString *const RKEventWarning = @"warning";
static NSString *const RKEventError   = @"error";

typedef void (^RKModuleReply)(NSDictionary *_Nullable result, NSError *_Nullable error);

// 交给每个模块，是模块访问 host 的唯一入口。
@interface RKModuleContext : NSObject
@property (nonatomic, readonly) NSString *identifier;
@property (nonatomic, readonly) dispatch_queue_t queue;
@property (nonatomic, readonly) os_log_t log;            // category = identifier
@property (nonatomic, readonly, getter=isServiceEnabled) BOOL serviceEnabled;

// 模块级持久化设置（property-list 值）。原子写入，重启/重新拉起后仍在。
// 保持精简：每次修改都会重写整个 state 文件。
- (nullable id)settingForKey:(NSString *)key;
- (void)setSetting:(nullable id)value forKey:(NSString *)key;

// 通知已连接的客户端有可见变化，由其重新查询。
- (void)postChange;

// 存放较大数据（队列、缓存）的私有目录，按需创建：
// <jbroot>/var/lib/residentd/modules/<identifier>/，重新拉起后仍在。
@property (nonatomic, readonly) NSString *dataDirectory;

// 像客户端一样调用另一个模块；目标同步回复时就在 `queue` 上同步完成。
// 模块间组合靠它，例如任意模块都可通过 net.link 的 "send" 上报服务端。
- (void)invokeModule:(NSString *)module
             command:(NSString *)command
           arguments:(nullable NSDictionary *)arguments
               reply:(RKModuleReply)reply;

// `module` 的 `command` 是否对远程调用方开放（见 +remoteCommands）。
- (BOOL)module:(NSString *)module allowsRemoteCommand:(NSString *)command;

// 所有模块开放的远程命令 {模块: [命令]}，net.link 在 hello 中上报给服务端。
@property (nonatomic, readonly) NSDictionary<NSString *, NSArray<NSString *> *> *remoteCommandMap;
// 远程命令的说明 {模块: {命令: 说明}}（见 +commandSchema），随 hello 上报，用于开放 API 的接口与文档。
@property (nonatomic, readonly) NSDictionary<NSString *, NSDictionary *> *commandSchemaMap;

// 发送事件给服务端（规范见 docs/events.md）。经 net.link 的持久化队列至少送达一次；
// net.link 未开启时先排队。类型名、级别或 payload 不合规时丢弃并记日志。
- (void)emitEvent:(NSString *)type level:(NSString *)level payload:(nullable NSDictionary *)payload;

// ---- 远程配置（docs/config.md）----
// 生效值：服务端下发的值，未下发时为 +configSchema 中的默认值；未声明的 key 返回 nil。
- (nullable id)config:(NSString *)key;

// 以下供 system.config 模块使用。
@property (nonatomic, readonly) NSDictionary<NSString *, NSArray<NSDictionary *> *> *configSchemaMap;
@property (nonatomic, readonly) int64_t configVersion;
@property (nonatomic, readonly) NSDictionary<NSString *, NSDictionary *> *configValues;   // 全部生效值
// 整体替换配置（未出现的项恢复默认）。先全部校验，任何一项不合规都不改动并返回 NO。
// 成功时 changed 为改动的 "模块.key"，并回调相关模块的 -configDidChange:。
- (BOOL)applyConfig:(NSDictionary *)config
            version:(int64_t)version
            changed:(NSArray<NSString *> *_Nullable *_Nullable)changed
              error:(NSError **)error;

- (NSError *)errorWithCode:(NSInteger)code description:(NSString *)description;
- (instancetype)init NS_UNAVAILABLE;
@end

@protocol RKModule <NSObject>
// 类反向域名、须保持稳定：持久化设置和客户端调用都以它为 key。
+ (NSString *)moduleIdentifier;
- (instancetype)initWithContext:(RKModuleContext *)context;

@optional
// 服务端可通过 net.link 的 "invoke" 调用的命令，默认为空。远程访问按命令显式开放，
// 服务端被攻破也只能做你暴露的事。本地客户端（app、rkctl）不受此限制。
+ (NSArray<NSString *> *)remoteCommands;

// 远程命令的说明，键为命令名（只对 +remoteCommands 中的命令生效）。服务端据此为每个命令
// 生成独立的开放 API 接口与文档，并在下发前校验参数。值是一个字典：
//   label   中文名称；desc 一句话说明（控制台指令卡片、下发面板、指令记录和开放 API 文档都直接用它们）
//   icon    可选，控制台图标：info · toggle · reset · upload · key · bolt · wifi · clock · check · shield ·
//           terminal · code · pulse · device；不写时按命令名推断（status→info、set→toggle…）
//   params  参数列表，每项与 configSchema 相同（key、type、label、desc、min/max、options…），
//           另加 required（默认 NO）。不声明 params 表示无参数
//   result  返回字段列表，每项 {key, type, desc}；type 为 bool | int | number | string | object | array
//   timeout 可选，执行上限（秒，1–3600，默认 120）。下发后超过这个时间仍无结果，服务端记为结果未知；
//           执行较久的命令务必声明，否则会被提前判定
// 只写对外有意义的字段；参数仍须在 -handleCommand: 里自行校验（本地客户端不经服务端）。
+ (NSDictionary<NSString *, NSDictionary *> *)commandSchema;

- (void)serviceDidStart;
- (void)serviceWillStop;

// 可由服务端下发的配置项。每项是一个字典：
//   key       名称，snake_case，带单位的加后缀（_s、_pct）
//   type      bool | int | number | string | enum
//   default   默认值（必填，须符合下面的约束）
//   label     中文名称，控制台显示；desc 可选的一句话说明
//   min / max 数值范围（int、number）；options 可选值（enum）；max_len 长度上限（string，默认 256）
// 只放「调参」类配置；连接地址、令牌等敏感项不要放进来。
+ (NSArray<NSDictionary *> *)configSchema;
// 配置变化（参数：改动的 key → 新的生效值），应立即生效，无需重启。在 queue 上调用。
- (void)configDidChange:(NSDictionary<NSString *, id> *)changed;
- (NSDictionary *)moduleStatus;
- (void)handleCommand:(NSString *)command
            arguments:(NSDictionary *)arguments
                reply:(RKModuleReply)reply;
@end

// 注册。在 +load（main 之前）调用，必须保持极简。
void RKRegisterModule(Class cls);
#define RK_REGISTER_MODULE() + (void)load { RKRegisterModule(self); }

NS_ASSUME_NONNULL_END
