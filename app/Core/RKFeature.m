// RKFeature.m — 说明见头文件。

#import "RKFeature.h"

static NSMutableArray<Class> *RKFeatureClasses(void) {
    static NSMutableArray *classes;
    if (!classes) classes = [NSMutableArray array];   // +load 是单线程的
    return classes;
}

void RKRegisterFeature(Class featureClass) {
    NSCAssert([featureClass isSubclassOfClass:[RKFeature class]],
              @"%@ must subclass RKFeature", featureClass);
    [RKFeatureClasses() addObject:featureClass];
}

NSArray<RKFeature *> *RKMakeRegisteredFeatures(RKClient *client) {
    NSMutableArray<RKFeature *> *all = [NSMutableArray array];
    for (Class cls in RKFeatureClasses()) {
        RKFeature *f = [(RKFeature *)[cls alloc] initWithClient:client];
        if (f) [all addObject:f];
    }
    [all sortUsingComparator:^NSComparisonResult(RKFeature *a, RKFeature *b) {
        if (a.style != b.style) return a.style < b.style ? NSOrderedAscending : NSOrderedDescending;
        NSInteger oa = [a.class order], ob = [b.class order];
        if (oa != ob) return oa < ob ? NSOrderedAscending : NSOrderedDescending;
        return [a.title localizedCompare:b.title];
    }];
    return all;
}

@implementation RKFeature

+ (NSInteger)order { return 100; }

- (instancetype)initWithClient:(RKClient *)client {
    if ((self = [super init])) _client = client;
    return self;
}

- (RKFeatureStyle)style { return RKFeatureStyleTile; }
- (NSString *)title { return NSStringFromClass(self.class); }
- (NSString *)symbolName { return @"square.dashed"; }
- (NSString *)subtitle { return nil; }
- (BOOL)isAvailable { return self.client.connected; }

- (BOOL)showsSwitch { return NO; }
- (BOOL)isOn { return NO; }
- (void)switchDidChange:(BOOL)on { [self setNeedsUpdate]; }

- (BOOL)isSelectable { return NO; }
- (UIViewController *)makeViewController { return nil; }

- (void)reload {}
- (void)setNeedsUpdate { [self.host featureNeedsUpdate:self]; }

@end
