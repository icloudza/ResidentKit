# rk-server 构建入口
#
#   make package              一键打包 Linux 版本：控制台前端 → Swagger → 去符号静态二进制 → UPX 压缩
#   make package GOARCH=arm64 打包 ARM 服务器版本
#   make package UPX=         不做 UPX 压缩
#
# 产物在 release/：rk-server-<版本>-linux-<架构>（单个可执行文件，控制台已内嵌）
#                  rk-server-<版本>-linux-<架构>.tar.gz（可执行文件 + config.example.yaml）

PKG      := ./cmd/rk-server
VERSION  ?= 1.1.0
GOOS     ?= linux
GOARCH   ?= amd64
UPX      ?= upx
NAME     := rk-server-$(VERSION)-$(GOOS)-$(GOARCH)
OUT      := release/$(NAME)

# -s -w 去掉符号表和调试信息；-trimpath、-buildid= 去掉本机路径，同样的源码构建结果一致。
LDFLAGS  := -s -w -buildid= -X github.com/icloudza/ResidentKit/server/internal/pkg/buildinfo.Version=$(VERSION)
GOFLAGS  := -trimpath -buildvcs=false
export GOPRIVATE := github.com/icloudza/*

.PHONY: all build linux package web swag test lint tidy clean

all: web swag build

build:                 ## 本机构建（开发用）
	go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o bin/rk-server $(PKG)

linux:                 ## 只编译 Linux 二进制到 bin/，不重新构建前端、不压缩
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o bin/rk-server-$(GOOS)-$(GOARCH) $(PKG)

package: web swag test ## 发布用：构建前端、文档，跑测试，编译并压缩
	@rm -rf release && mkdir -p release
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $(OUT) $(PKG)
	@raw=$$(wc -c < $(OUT)); \
	if [ -n "$(UPX)" ] && command -v $(UPX) >/dev/null; then \
		$(UPX) -q --best --lzma $(OUT) >/dev/null && $(UPX) -q -t $(OUT) >/dev/null; \
	elif [ -n "$(UPX)" ]; then \
		echo "!! 没有找到 upx（brew install upx），跳过压缩"; \
	fi; \
	echo "==> $(OUT)  $$((raw / 1024)) KB → $$(( $$(wc -c < $(OUT)) / 1024 )) KB"
	@mkdir -p release/$(NAME).d && cp $(OUT) release/$(NAME).d/rk-server && cp configs/config.example.yaml release/$(NAME).d/ \
		&& tar -C release/$(NAME).d -czf release/$(NAME).tar.gz rk-server config.example.yaml && rm -rf release/$(NAME).d
	@echo "==> release/$(NAME).tar.gz"

web:                   ## 构建控制台前端到 web/dist（内嵌进二进制）
	cd web && npm ci --no-audit --no-fund && npm run build

swag:                  ## 生成 Swagger 文档
	swag init -g internal/handler/v1/auth.go -o docs --parseInternal --outputTypes go,json

test:
	go test -race ./...

lint:
	golangci-lint run ./...

tidy:
	go mod tidy

clean:
	rm -rf bin release
