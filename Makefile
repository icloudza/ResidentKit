# 聚合工程：构建 daemon（tool）和 UI App，连同 layout/ 中的 LaunchDaemon plist
# 一起打成一个 deb。
#
#   make package                       → rootful（传统 /）
#   make package THEOS_PACKAGE_SCHEME=rootless   → Dopamine 等（/var/jb）
#   make package THEOS_PACKAGE_SCHEME=roothide    → RootHide（需 roothide 版 theos）

include $(THEOS)/makefiles/common.mk

SUBPROJECTS += daemon
SUBPROJECTS += app

include $(THEOS)/makefiles/aggregate.mk
