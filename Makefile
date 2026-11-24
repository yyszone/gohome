APP_NAME := gohome
GO ?= go
BUILD_DIR := build
DIST_DIR := dist
MAIN_PKG := ./cmd/gohome

# 版本号使用 Git 提交总数自动生成；源码版本不写死补丁号。
# 每新增一个 Git commit，下一次 make 的 CommitCount / VERSION 自动 +1。
# 注意：git rev-list --count 是提交次数，不是每执行一次 make 就 +1。
VERSION_PREFIX ?= 0.2
BUILD_DATE := $(shell date +%Y%m%d)
COMMIT_COUNT := $(shell git rev-list --count HEAD 2>/dev/null || echo 0)
GIT_HASH := $(shell git rev-parse --short HEAD 2>/dev/null || echo no-git)
GIT_MESSAGE := $(shell git log -1 --pretty=%s 2>/dev/null | tr '\n' ' ' | sed "s/'//g" | cut -c1-120)
ifeq ($(strip $(GIT_MESSAGE)),)
GIT_MESSAGE := source-archive-no-git-metadata
endif
VERSION := $(VERSION_PREFIX).$(COMMIT_COUNT)
LDFLAGS := -s -w -X 'gohome/internal/app.version=$(VERSION)' -X 'gohome/internal/app.buildDate=$(BUILD_DATE)' -X 'gohome/internal/app.commitCount=$(COMMIT_COUNT)' -X 'gohome/internal/app.gitHash=$(GIT_HASH)' -X 'gohome/internal/app.gitMessage=$(GIT_MESSAGE)'

.PHONY: all build run dev test vet check release linux windows clean help

# 默认 make：先执行测试和静态检查，再一次生成 Linux / Windows amd64。
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

release: check linux windows

linux:
	@mkdir -p $(DIST_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(DIST_DIR)/$(APP_NAME)-linux-amd64 $(MAIN_PKG)

windows:
	@mkdir -p $(DIST_DIR)
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(DIST_DIR)/$(APP_NAME)-windows-amd64.exe $(MAIN_PKG)

clean:
	rm -rf $(BUILD_DIR) $(DIST_DIR)

help:
	@printf '%s\n' 'make          测试并同时编译 Linux/Windows x64' 'make build    编译当前平台到 build/' 'make run      编译并运行当前平台' 'make dev      go run 开发模式' 'make test     运行单元测试' 'make vet      Go 静态检查' 'make release  测试并交叉编译 Linux/Windows x64' 'make linux    只编译 Linux x64' 'make windows  只编译 Windows x64' 'make clean    删除构建产物' 'make help     显示帮助'
