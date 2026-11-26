APP_NAME := gohome
GO ?= go
BUILD_DIR := build
DIST_DIR := dist
MAIN_PKG := ./cmd/gohome

# 有 .git 时按 Git 提交总数生成版本；源码 ZIP 无 .git 时使用 SOURCE_VERSION。
# 每新增一个 Git commit，下一次 make 的 CommitCount / VERSION 自动 +1。
# 注意：git rev-list --count 是提交次数，不是每执行一次 make 就 +1。
VERSION_PREFIX ?= 0.2
# 源码 ZIP 不包含 .git：使用源码声明的版本，避免退化成 0.2.0。
SOURCE_VERSION ?= 0.2.28
BUILD_DATE := $(shell date +%Y%m%d)
GIT_ROOT := $(shell git rev-parse --show-toplevel 2>/dev/null)
ifneq ($(strip $(GIT_ROOT)),)
COMMIT_COUNT := $(shell git rev-list --count HEAD 2>/dev/null || echo 0)
GIT_HASH := $(shell git rev-parse --short HEAD 2>/dev/null || echo no-git)
GIT_MESSAGE := $(shell git log -1 --pretty=%s 2>/dev/null | tr '\n' ' ' | sed "s/'//g" | cut -c1-120)
VERSION := $(VERSION_PREFIX).$(COMMIT_COUNT)
else
COMMIT_COUNT := 0
GIT_HASH := source
GIT_MESSAGE := source-archive-no-git-metadata
VERSION := $(SOURCE_VERSION)
endif
LDFLAGS := -s -w -X 'gohome/internal/app.version=$(VERSION)' -X 'gohome/internal/app.buildDate=$(BUILD_DATE)' -X 'gohome/internal/app.commitCount=$(COMMIT_COUNT)' -X 'gohome/internal/app.gitHash=$(GIT_HASH)' -X 'gohome/internal/app.gitMessage=$(GIT_MESSAGE)'

.PHONY: all build run dev test vet check release linux linux-arm32 linux-armv7 linux-arm64 windows armbian deploy clean help

# 默认 make：先执行测试和静态检查，再生成 Linux amd64 / ARMv7 / ARM64 与 Windows amd64。
all: release

build:
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(APP_NAME) $(MAIN_PKG)

run: build
	./$(BUILD_DIR)/$(APP_NAME)

dev:
	$(GO) run $(MAIN_PKG)

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

check: test vet

release: check linux linux-arm32 linux-arm64 windows

linux:
	@mkdir -p $(DIST_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(DIST_DIR)/$(APP_NAME)-linux-amd64 $(MAIN_PKG)

# 32 位 ARM Linux（Armbian 常见 armhf / ARMv7）。
# 如确实使用 ARMv6，可以通过 ARM32_GOARM=6 覆盖，但默认面向 ARMv7。
ARM32_GOARM ?= 7

linux-arm32:
	@mkdir -p $(DIST_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=$(ARM32_GOARM) $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(DIST_DIR)/$(APP_NAME)-linux-armv7 $(MAIN_PKG)

# 64 位 ARM Linux（Armbian aarch64）。
linux-arm64:
	@mkdir -p $(DIST_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(DIST_DIR)/$(APP_NAME)-linux-arm64 $(MAIN_PKG)

# 兼容更直观的目标名。
linux-armv7: linux-arm32

# 仅构建两种 Armbian ARM 架构（仍执行测试和静态检查）。
armbian: check linux-arm32 linux-arm64

# 一条命令安全部署：make deploy DEPLOY_HOST=root@192.168.31.63 DEPLOY_ARCH=arm64
DEPLOY_HOST ?=
DEPLOY_ARCH ?= arm64
DEPLOY_PATH ?= /root/gohome
DEPLOY_SERVICE ?= gohome
ifeq ($(DEPLOY_ARCH),arm64)
DEPLOY_TARGET := linux-arm64
DEPLOY_BINARY := $(APP_NAME)-linux-arm64
else ifeq ($(DEPLOY_ARCH),armv7)
DEPLOY_TARGET := linux-arm32
DEPLOY_BINARY := $(APP_NAME)-linux-armv7
else ifeq ($(DEPLOY_ARCH),amd64)
DEPLOY_TARGET := linux
DEPLOY_BINARY := $(APP_NAME)-linux-amd64
else
DEPLOY_TARGET :=
DEPLOY_BINARY :=
endif

deploy:
	@test -n "$(DEPLOY_HOST)" || { echo '请指定 DEPLOY_HOST，例如 root@192.168.31.63'; exit 2; }
	@test -n "$(DEPLOY_TARGET)" || { echo 'DEPLOY_ARCH 仅支持 arm64、armv7、amd64'; exit 2; }
	@$(MAKE) check
	@$(MAKE) $(DEPLOY_TARGET)
	@DEPLOY_HOST='$(DEPLOY_HOST)' DEPLOY_ARCH='$(DEPLOY_ARCH)' DEPLOY_PATH='$(DEPLOY_PATH)' DEPLOY_SERVICE='$(DEPLOY_SERVICE)' DEPLOY_BINARY='$(DEPLOY_BINARY)' sh scripts/deploy.sh

windows:
	@mkdir -p $(DIST_DIR)
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(DIST_DIR)/$(APP_NAME)-windows-amd64.exe $(MAIN_PKG)

clean:
	rm -rf $(BUILD_DIR) $(DIST_DIR)

help:
	@printf '%s\n' 'make                 测试并编译 Linux x64/ARMv7/ARM64 + Windows x64（无 .git 时使用 SOURCE_VERSION）' 'make build           编译当前平台到 build/' 'make run             编译并运行当前平台' 'make dev             go run 开发模式' 'make test            运行单元测试' 'make vet             Go 静态检查' 'make release         测试并交叉编译全部发布目标' 'make linux           只编译 Linux x64' 'make linux-arm32     编译 Linux ARMv7 32 位' 'make linux-arm64     编译 Linux ARM64 64 位' 'make armbian         测试并编译 Armbian ARM32 + ARM64' 'make deploy DEPLOY_HOST=root@IP DEPLOY_ARCH=arm64 远程部署并自动回滚' 'make windows         只编译 Windows x64' 'make clean           删除构建产物' 'make help            显示帮助'
