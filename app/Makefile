TARGET := iphone:clang:latest:14.0
ARCHS := arm64 arm64e

include $(THEOS)/makefiles/common.mk

# 与 daemon 共用版本号（取自包的 control 文件），升级后 App 据此发现旧 daemon。
RK_VERSION := $(shell awk '/^Version:/{print $$2}' ../control)

APPLICATION_NAME = ResidentKit

# Features/ 下的功能项自动纳入编译（见 Core/RKFeature.h）。
ResidentKit_FILES = $(wildcard *.m) $(wildcard Core/*.m) $(wildcard UI/*.m) $(wildcard Features/*.m)
ResidentKit_CFLAGS = -fobjc-arc -DRK_VERSION='"$(RK_VERSION)"'
ResidentKit_FRAMEWORKS = UIKit Foundation QuartzCore CoreGraphics AVFoundation
ResidentKit_CODESIGN_FLAGS = -Sentitlements.plist

include $(THEOS)/makefiles/application.mk
