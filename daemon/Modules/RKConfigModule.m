// RKConfigModule — 远程配置（docs/config.md）。
//
// 各模块在 +configSchema 中声明可配置项；服务端经 net.link 调用本模块：
//   get    → { version, schema: {模块: [配置项]}, values: {模块: {key: 生效值}} }
//   apply  { version, config: {模块: {key: 值}} } → 整体替换，先全部校验再生效
// 生效后发事件 system.config.applied，服务端据此确认设备已同步到哪个版本。

#import "RKModule.h"
#import "../../common/RKProtocol.h"

@interface RKConfigModule : NSObject <RKModule>
@end

@implementation RKConfigModule {
    RKModuleContext *_ctx;
}

RK_REGISTER_MODULE()

+ (NSString *)moduleIdentifier { return @"system.config"; }
+ (NSArray<NSString *> *)remoteCommands { return @[ @"get", @"apply" ]; }

+ (NSDictionary<NSString *, NSDictionary *> *)commandSchema {
    return @{
        @"get": @{ @"label": @"读取配置", @"desc": @"设备当前生效的配置与全部可配置项",
                   @"result": @[ @{ @"key": @"version", @"type": @"int", @"desc": @"已生效的配置版本" },
                                 @{ @"key": @"schema", @"type": @"object", @"desc": @"可配置项 {模块: [配置项]}" },
                                 @{ @"key": @"values", @"type": @"object", @"desc": @"生效值 {模块: {key: 值}}" } ] },
        @"apply": @{ @"label": @"应用配置", @"desc": @"整体替换配置（由控制台「配置」卡片下发）",
                     @"params": @[ @{ @"key": @"version", @"type": @"int", @"label": @"配置版本", @"required": @YES, @"desc": @"正整数" },
                                   @{ @"key": @"config", @"type": @"object", @"label": @"配置", @"required": @YES, @"desc": @"{模块: {key: 值}}" } ],
                     @"result": @[ @{ @"key": @"version", @"type": @"int", @"desc": @"已生效的版本" },
                                   @{ @"key": @"changed", @"type": @"array", @"desc": @"改动的「模块.key」" } ] },
    };
}

- (instancetype)initWithContext:(RKModuleContext *)context {
    if ((self = [super init])) _ctx = context;
    return self;
}

- (NSDictionary *)moduleStatus {
    return @{ @"version": @(_ctx.configVersion), @"values": _ctx.configValues };
}

- (void)handleCommand:(NSString *)command arguments:(NSDictionary *)args reply:(RKModuleReply)reply {
    if ([command isEqualToString:@"get"]) {
        reply(@{ @"version": @(_ctx.configVersion), @"schema": _ctx.configSchemaMap, @"values": _ctx.configValues }, nil);
        return;
    }
    if (![command isEqualToString:@"apply"]) {
        reply(nil, [_ctx errorWithCode:RKErrorUnknownCommand description:[@"未知命令 " stringByAppendingString:command]]);
        return;
    }
    NSNumber *version = args[@"version"];
    NSDictionary *config = args[@"config"];
    if (![version isKindOfClass:NSNumber.class] || version.longLongValue <= 0 || ![config isKindOfClass:NSDictionary.class]) {
        reply(nil, [_ctx errorWithCode:RKErrorInvalidArguments description:@"apply 需要 version（正整数）与 config（对象）"]);
        return;
    }
    NSArray *changed = nil;
    NSError *err = nil;
    if (![_ctx applyConfig:config version:version.longLongValue changed:&changed error:&err]) {
        os_log_error(_ctx.log, "config %lld rejected: %{public}@", version.longLongValue, err.localizedDescription);
        [_ctx emitEvent:@"system.config.rejected" level:RKEventError
                payload:@{ @"version": version, @"error": err.localizedDescription ?: @"" }];
        reply(nil, err);
        return;
    }
    os_log(_ctx.log, "config %lld applied, %lu changed", version.longLongValue, (unsigned long)changed.count);
    [_ctx emitEvent:@"system.config.applied" level:changed.count ? RKEventNotice : RKEventInfo
            payload:@{ @"version": version, @"changed": changed ?: @[] }];
    [_ctx postChange];
    reply(@{ @"version": version, @"changed": changed ?: @[] }, nil);
}

@end
