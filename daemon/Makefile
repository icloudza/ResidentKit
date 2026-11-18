TARGET := iphone:clang:latest:14.0
# rootless 下 arm64 覆盖 A12..A16；arm64e 越狱也可用 arm64e。
ARCHS := arm64 arm64e

include $(THEOS)/makefiles/common.mk

# 版本号唯一来源：包 control 文件的 Version: 行。
RK_VERSION := $(shell awk '/^Version:/{print $$2}' ../control)

TOOL_NAME = residentd rk-updater

# Modules/ 下的模块自动编入（见 RKModule.h）。
residentd_FILES = $(wildcard *.m) $(wildcard Modules/*.m)
residentd_CFLAGS = -fobjc-arc -I. -DRK_VERSION='"$(RK_VERSION)"'
residentd_FRAMEWORKS = Foundation Network IOKit Security
residentd_CODESIGN_FLAGS = -Sentitlements.plist
# 装到越狱前缀下；rootless/roothide 打包时会自动改写前缀。
residentd_INSTALL_PATH = /usr/libexec

# OTA 安装助手：独立 launchd 任务，负责 dpkg、健康检查与回滚。
rk-updater_FILES = updater/main.m
rk-updater_CFLAGS = -fobjc-arc -DRK_VERSION='"$(RK_VERSION)"'
rk-updater_FRAMEWORKS = Foundation
rk-updater_CODESIGN_FLAGS = -Supdater/entitlements.plist
rk-updater_INSTALL_PATH = /usr/libexec

include $(THEOS)/makefiles/tool.mk
