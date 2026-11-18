// RKModuleHost.m — 说明见头文件。

#import "RKModuleHost.h"
#import "../common/RKProtocol.h"
#import <notify.h>

static NSMutableArray<Class> *RKModuleClasses(void) {
    static NSMutableArray *classes;
    if (!classes) classes = [NSMutableArray array];   // +load 是单线程的
    return classes;
}

void RKRegisterModule(Class cls) {
    [RKModuleClasses() addObject:cls];
}

#pragma mark - 模块上下文
@interface RKModuleContext ()
@property (nonatomic, weak) RKModuleHost *host;
- (instancetype)initWithIdentifier:(NSString *)identifier
                             queue:(dispatch_queue_t)queue
                              host:(RKModuleHost *)host;
@end

@interface RKModuleHost ()
- (nullable id)configForModule:(NSString *)module key:(NSString *)key;
@property (nonatomic, readonly) NSDictionary<NSString *, NSArray<NSDictionary *> *> *configSchemaMap;
@property (nonatomic, readonly) int64_t configVersion;
@property (nonatomic, readonly) NSDictionary *configValues;
- (BOOL)applyConfig:(NSDictionary *)config version:(int64_t)version
            changed:(NSArray **)changed error:(NSError **)error;
- (nullable id)settingForModule:(NSString *)module key:(NSString *)key;
- (void)setSetting:(nullable id)value forModule:(NSString *)module key:(NSString *)key;
@property (nonatomic, readonly) BOOL serviceEnabled;
@property (nonatomic, readonly) NSString *dataRoot;
@end

@implementation RKModuleContext

- (instancetype)initWithIdentifier:(NSString *)identifier
                             queue:(dispatch_queue_t)queue
                              host:(RKModuleHost *)host {
    if ((self = [super init])) {
        _identifier = [identifier copy];
        _queue = queue;
        _host = host;
        _log = os_log_create(RK_LOG_SUBSYSTEM, identifier.UTF8String);
    }
    return self;
}

- (BOOL)isServiceEnabled { return self.host.serviceEnabled; }

- (id)settingForKey:(NSString *)key {
    dispatch_assert_queue(_queue);
    return [self.host settingForModule:_identifier key:key];
}

- (void)setSetting:(id)value forKey:(NSString *)key {
    dispatch_assert_queue(_queue);
    [self.host setSetting:value forModule:_identifier key:key];
}

- (void)postChange { notify_post(RK_DARWIN_CHANGED); }

- (NSString *)dataDirectory {
    NSString *dir = [self.host.dataRoot stringByAppendingPathComponent:_identifier];
    [[NSFileManager defaultManager] createDirectoryAtPath:dir withIntermediateDirectories:YES
                                               attributes:nil error:nil];
    return dir;
}

- (void)invokeModule:(NSString *)module command:(NSString *)command
           arguments:(NSDictionary *)arguments reply:(RKModuleReply)reply {
    dispatch_assert_queue(_queue);
    RKModuleHost *host = self.host;
    if (!host) { reply(nil, [self errorWithCode:RKErrorUnknownModule description:@"host gone"]); return; }
    [host invokeModule:module command:command arguments:arguments reply:reply];
}

- (BOOL)module:(NSString *)module allowsRemoteCommand:(NSString *)command {
    return [self.host module:module allowsRemoteCommand:command];
}

- (NSDictionary *)remoteCommandMap { return self.host.remoteCommandMap ?: @{}; }
- (NSDictionary *)commandSchemaMap { return self.host.commandSchemaMap ?: @{}; }

- (void)emitEvent:(NSString *)type level:(NSString *)level payload:(NSDictionary *)payload {
    dispatch_assert_queue(_queue);
    [self.host emitEvent:type level:level payload:payload];
}

- (id)config:(NSString *)key {
    dispatch_assert_queue(_queue);
    return [self.host configForModule:_identifier key:key];
}

- (NSDictionary *)configSchemaMap { return self.host.configSchemaMap ?: @{}; }
- (int64_t)configVersion { return self.host.configVersion; }
- (NSDictionary *)configValues { return self.host.configValues ?: @{}; }

- (BOOL)applyConfig:(NSDictionary *)config version:(int64_t)version
            changed:(NSArray<NSString *> **)changed error:(NSError **)error {
    dispatch_assert_queue(_queue);
    return [self.host applyConfig:config version:version changed:changed error:error];
}

- (NSError *)errorWithCode:(NSInteger)code description:(NSString *)description {
    return [NSError errorWithDomain:RKErrorDomain code:code
                           userInfo:@{ NSLocalizedDescriptionKey: description }];
}

@end

#pragma mark - 配置校验

static NSString *const RKConfigKey = @"@config";   // settings 中保存配置的位置，不会与模块 ID 冲突

static BOOL RKIsBool(id v) { return [v isKindOfClass:NSNumber.class] && CFGetTypeID((__bridge CFTypeRef)v) == CFBooleanGetTypeID(); }

// 按声明校验并规范化一个值；不合规返回 nil 并给出原因。
static id RKNormalizeConfigValue(NSDictionary *item, id v, NSString **why) {
    NSString *type = item[@"type"];
    if ([type isEqualToString:@"bool"]) {
        if (RKIsBool(v) || ([v isKindOfClass:NSNumber.class] && ([v intValue] == 0 || [v intValue] == 1)))
            return @([v boolValue]);
        *why = @"应为开关（true / false）";
        return nil;
    }
    if ([type isEqualToString:@"int"] || [type isEqualToString:@"number"]) {
        if (![v isKindOfClass:NSNumber.class] || RKIsBool(v)) { *why = @"应为数字"; return nil; }
        double d = [v doubleValue];
        if (isnan(d) || isinf(d)) { *why = @"应为数字"; return nil; }
        BOOL isInt = [type isEqualToString:@"int"];
        if (isInt && d != floor(d)) { *why = @"应为整数"; return nil; }
        NSNumber *min = item[@"min"], *max = item[@"max"];
        if ((min && d < min.doubleValue) || (max && d > max.doubleValue)) {
            *why = [NSString stringWithFormat:@"应在 %@–%@ 之间", min ?: @"", max ?: @""];
            return nil;
        }
        return isInt ? @((long long)d) : @(d);
    }
    if ([type isEqualToString:@"string"]) {
        NSUInteger maxLen = [item[@"max_len"] unsignedIntegerValue] ?: 256;
        if (![v isKindOfClass:NSString.class] || [v length] > maxLen) {
            *why = [NSString stringWithFormat:@"应为不超过 %lu 字的文本", (unsigned long)maxLen];
            return nil;
        }
        return v;
    }
    if ([type isEqualToString:@"enum"]) {
        if ([v isKindOfClass:NSString.class] && [item[@"options"] containsObject:v]) return v;
        *why = [NSString stringWithFormat:@"应为 %@ 之一", [item[@"options"] componentsJoinedByString:@" / "]];
        return nil;
    }
    *why = @"未知类型";
    return nil;
}

#pragma mark - 模块宿主
@implementation RKModuleHost {
    NSDictionary<NSString *, NSArray<NSDictionary *> *> *_schemaList;   // 模块 → 配置项（声明顺序）
    NSDictionary<NSString *, NSDictionary<NSString *, NSDictionary *> *> *_schemas;   // 模块 → key → 配置项
    dispatch_queue_t _queue;
    void (^_persist)(void);
    NSMutableDictionary<NSString *, NSMutableDictionary *> *_settings;
    NSMutableDictionary<NSString *, id<RKModule>> *_modules;
    NSArray<NSString *> *_order;
    BOOL _loading;   // 模块可能在 -init 里设默认值，由持有者之后统一保存一次
}

- (instancetype)initWithQueue:(dispatch_queue_t)queue
                     settings:(NSDictionary *)settings
                     dataRoot:(NSString *)dataRoot
                      persist:(void (^)(void))persist {
    if ((self = [super init])) {
        _queue = queue;
        _dataRoot = [dataRoot copy];
        _persist = [persist copy];
        _settings = [NSMutableDictionary dictionary];
        [settings enumerateKeysAndObjectsUsingBlock:^(id k, id v, BOOL *stop) {
            if ([k isKindOfClass:NSString.class] && [v isKindOfClass:NSDictionary.class])
                self->_settings[k] = [v mutableCopy];
        }];
        _modules = [NSMutableDictionary dictionary];
        NSMutableArray *order = [NSMutableArray array];
        _loading = YES;
        dispatch_sync(queue, ^{
            for (Class cls in RKModuleClasses()) {
                NSString *ident = [(Class<RKModule>)cls moduleIdentifier];
                if (self->_modules[ident]) {
                    os_log_error(OS_LOG_DEFAULT, "residentd: duplicate module %{public}@ ignored", ident);
                    continue;
                }
                RKModuleContext *ctx = [[RKModuleContext alloc] initWithIdentifier:ident
                                                                             queue:queue
                                                                              host:self];
                id<RKModule> m = [(id<RKModule>)[cls alloc] initWithContext:ctx];
                if (!m) continue;
                self->_modules[ident] = m;
                [order addObject:ident];
            }
        });
        _order = [order copy];
        [self loadSchemas];
        _loading = NO;
    }
    return self;
}

- (NSArray<NSString *> *)moduleIdentifiers { return _order; }

#pragma mark - 配置

// 收集各模块的配置声明；声明本身不合规（缺字段、默认值不满足约束）的项跳过并记日志。
- (void)loadSchemas {
    NSMutableDictionary *list = [NSMutableDictionary dictionary], *map = [NSMutableDictionary dictionary];
    for (NSString *ident in _order) {
        Class cls = [(NSObject *)_modules[ident] class];
        if (![cls respondsToSelector:@selector(configSchema)]) continue;
        NSMutableArray *items = [NSMutableArray array];
        NSMutableDictionary *byKey = [NSMutableDictionary dictionary];
        for (NSDictionary *item in [(Class<RKModule>)cls configSchema]) {
            NSString *key = item[@"key"], *why = nil;
            if (![key isKindOfClass:NSString.class] || !item[@"label"] || byKey[key] ||
                !RKNormalizeConfigValue(item, item[@"default"], &why)) {
                os_log_error(OS_LOG_DEFAULT, "residentd: bad config item %{public}@.%{public}@ %{public}@", ident, key, why ?: @"");
                continue;
            }
            [items addObject:item];
            byKey[key] = item;
        }
        if (items.count) { list[ident] = items; map[ident] = byKey; }
    }
    _schemaList = list;
    _schemas = map;
}

- (NSDictionary *)configSchemaMap { return _schemaList; }

- (int64_t)configVersion { return [_settings[RKConfigKey][@"version"] longLongValue]; }

- (id)configForModule:(NSString *)module key:(NSString *)key {
    NSDictionary *item = _schemas[module][key];
    if (!item) return nil;
    id v = _settings[RKConfigKey][@"values"][module][key];
    NSString *why = nil;
    // 已保存的值在模块升级后可能不再合规（范围变了），此时回退默认值。
    return (v ? RKNormalizeConfigValue(item, v, &why) : nil) ?: item[@"default"];
}

- (NSDictionary *)configValues {
    NSMutableDictionary *out = [NSMutableDictionary dictionary];
    [_schemas enumerateKeysAndObjectsUsingBlock:^(NSString *mod, NSDictionary *items, BOOL *stop) {
        NSMutableDictionary *m = [NSMutableDictionary dictionary];
        for (NSString *key in items) m[key] = [self configForModule:mod key:key];
        out[mod] = m;
    }];
    return out;
}

- (BOOL)applyConfig:(NSDictionary *)config version:(int64_t)version
            changed:(NSArray **)changedOut error:(NSError **)error {
    dispatch_assert_queue(_queue);
    NSError *(^fail)(NSString *) = ^(NSString *msg) {
        return [NSError errorWithDomain:RKErrorDomain code:RKErrorInvalidArguments
                               userInfo:@{ NSLocalizedDescriptionKey: msg }];
    };
    if (![config isKindOfClass:NSDictionary.class]) { if (error) *error = fail(@"config 应为对象"); return NO; }

    // 1. 全部校验；只保存与默认值不同的项。
    NSMutableDictionary *values = [NSMutableDictionary dictionary];
    for (NSString *mod in config) {
        NSDictionary *items = _schemas[mod], *given = config[mod];
        if (!items) { if (error) *error = fail([NSString stringWithFormat:@"模块 %@ 没有可配置项", mod]); return NO; }
        if (![given isKindOfClass:NSDictionary.class]) { if (error) *error = fail([NSString stringWithFormat:@"%@ 应为对象", mod]); return NO; }
        NSMutableDictionary *m = [NSMutableDictionary dictionary];
        for (NSString *key in given) {
            NSDictionary *item = items[key];
            if (!item) { if (error) *error = fail([NSString stringWithFormat:@"%@ 没有配置项 %@", mod, key]); return NO; }
            NSString *why = nil;
            id v = RKNormalizeConfigValue(item, given[key], &why);
            if (!v) { if (error) *error = fail([NSString stringWithFormat:@"%@（%@.%@）%@", item[@"label"], mod, key, why]); return NO; }
            if (![v isEqual:item[@"default"]]) m[key] = v;
        }
        if (m.count) values[mod] = m;
    }

    // 2. 保存前记下旧的生效值，保存后对比得出改动。
    NSDictionary *before = self.configValues;
    NSMutableDictionary *d = _settings[RKConfigKey] ?: [NSMutableDictionary dictionary];
    _settings[RKConfigKey] = d;
    d[@"version"] = @(version);
    d[@"values"] = values;
    if (_persist) _persist();

    NSMutableArray *changed = [NSMutableArray array];
    NSDictionary *after = self.configValues;
    for (NSString *mod in _order) {
        NSMutableDictionary *diff = [NSMutableDictionary dictionary];
        [after[mod] enumerateKeysAndObjectsUsingBlock:^(NSString *key, id v, BOOL *stop) {
            if (![before[mod][key] isEqual:v]) diff[key] = v;
        }];
        if (!diff.count) continue;
        for (NSString *key in diff) [changed addObject:[NSString stringWithFormat:@"%@.%@", mod, key]];
        id<RKModule> m = _modules[mod];
        if ([m respondsToSelector:@selector(configDidChange:)]) [m configDidChange:diff];
    }
    if (changedOut) *changedOut = changed;
    return YES;
}

- (void)emitEvent:(NSString *)type level:(NSString *)level payload:(NSDictionary *)payload {
    dispatch_assert_queue(_queue);
    NSDictionary *args = @{ @"type": type ?: @"", @"level": level ?: RKEventInfo, @"payload": payload ?: @{} };
    [self invokeModule:@"net.link" command:@"send" arguments:args reply:^(NSDictionary *r, NSError *e) {
        if (e) os_log_error(OS_LOG_DEFAULT, "residentd: event %{public}@ dropped: %{public}@", type, e.localizedDescription);
    }];
}

- (NSDictionary *)remoteCommandMap {
    NSMutableDictionary *map = [NSMutableDictionary dictionary];
    for (NSString *ident in _order) {
        Class cls = [(NSObject *)_modules[ident] class];
        if ([cls respondsToSelector:@selector(remoteCommands)]) {
            NSArray *cmds = [(Class<RKModule>)cls remoteCommands];
            if (cmds.count) map[ident] = cmds;
        }
    }
    return map;
}

- (NSDictionary *)commandSchemaMap {
    NSMutableDictionary *map = [NSMutableDictionary dictionary];
    for (NSString *ident in _order) {
        Class cls = [(NSObject *)_modules[ident] class];
        if (![cls respondsToSelector:@selector(remoteCommands)] || ![cls respondsToSelector:@selector(commandSchema)]) continue;
        NSArray *cmds = [(Class<RKModule>)cls remoteCommands];
        NSMutableDictionary *m = [NSMutableDictionary dictionary];
        [[(Class<RKModule>)cls commandSchema] enumerateKeysAndObjectsUsingBlock:^(NSString *cmd, NSDictionary *spec, BOOL *stop) {
            if ([cmds containsObject:cmd] && [spec isKindOfClass:NSDictionary.class]) m[cmd] = spec;
        }];
        if (m.count) map[ident] = m;
    }
    return map;
}

- (BOOL)module:(NSString *)module allowsRemoteCommand:(NSString *)command {
    Class cls = [(NSObject *)_modules[module] class];
    if (!cls || ![cls respondsToSelector:@selector(remoteCommands)]) return NO;
    return [[(Class<RKModule>)cls remoteCommands] containsObject:command];
}

- (NSDictionary *)settings {
    NSMutableDictionary *copy = [NSMutableDictionary dictionaryWithCapacity:_settings.count];
    [_settings enumerateKeysAndObjectsUsingBlock:^(NSString *k, NSDictionary *v, BOOL *stop) {
        if (v.count) copy[k] = [v copy];
    }];
    return copy;
}

- (id)settingForModule:(NSString *)module key:(NSString *)key {
    return _settings[module][key];
}

- (void)setSetting:(id)value forModule:(NSString *)module key:(NSString *)key {
    NSMutableDictionary *d = _settings[module];
    if (!d) _settings[module] = d = [NSMutableDictionary dictionary];
    if ((value == nil && d[key] == nil) || [d[key] isEqual:value]) return;
    d[key] = value;
    if (_persist && !_loading) _persist();
}

- (void)setServiceEnabled:(BOOL)enabled {
    dispatch_assert_queue(_queue);
    if (_serviceEnabled == enabled) return;
    _serviceEnabled = enabled;
    // 按注册顺序启动，逆序停止。
    NSArray *ids = enabled ? _order : _order.reverseObjectEnumerator.allObjects;
    for (NSString *ident in ids) {
        id<RKModule> m = _modules[ident];
        if (enabled && [m respondsToSelector:@selector(serviceDidStart)]) [m serviceDidStart];
        if (!enabled && [m respondsToSelector:@selector(serviceWillStop)]) [m serviceWillStop];
    }
}

- (void)invokeModule:(NSString *)module
             command:(NSString *)command
           arguments:(NSDictionary *)arguments
               reply:(RKModuleReply)reply {
    dispatch_assert_queue(_queue);
    id<RKModule> m = _modules[module];
    if (!m) {
        reply(nil, [NSError errorWithDomain:RKErrorDomain code:RKErrorUnknownModule
            userInfo:@{ NSLocalizedDescriptionKey:
                [NSString stringWithFormat:@"未知模块 %@", module] }]);
        return;
    }
    if ([command isEqualToString:@"status"]) {
        reply([m respondsToSelector:@selector(moduleStatus)] ? [m moduleStatus] : @{}, nil);
        return;
    }
    if (![m respondsToSelector:@selector(handleCommand:arguments:reply:)]) {
        reply(nil, [NSError errorWithDomain:RKErrorDomain code:RKErrorUnknownCommand
            userInfo:@{ NSLocalizedDescriptionKey:
                [NSString stringWithFormat:@"模块 %@ 不支持命令 %@", module, command] }]);
        return;
    }
    // 保证恰好回复一次，防止模块重复调用 reply。
    __block BOOL replied = NO;
    [m handleCommand:command arguments:arguments ?: @{} reply:^(NSDictionary *r, NSError *e) {
        if (replied) return;
        replied = YES;
        reply(r, e);
    }];
}

@end
