APP_NAME := gohome
VERSION := 0.2.11
GO ?= go
BUILD_DIR := build
DIST_DIR := dist
MAIN_PKG := ./cmd/gohome
LDFLAGS := -s -w -X gohome/internal/app.version=$(VERSION)
HOST_OS := $(shell $(GO) env GOOS)
HOST_EXE :=
ifeq ($(HOST_OS),windows)
HOST_EXE := .exe
endif

.PHONY: all build run dev test vet check release linux windows clean help

# 默认一条 make 即执行测试、静态检查，并同时生成 Linux/Windows x64。
all: release

build:
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(APP_NAME)$(HOST_EXE) $(MAIN_PKG)

run: build
	./$(BUILD_DIR)/$(APP_NAME)$(HOST_EXE)

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
	@printf '%s\n' 'make          测试并同时编译 Linux/Windows x64（默认）' 'make build    编译当前平台到 build/' 'make run      编译并运行当前平台' 'make dev      go run 开发模式' 'make test     运行单元测试' 'make vet      Go 静态检查' 'make release  测试并交叉编译 Linux/Windows x64' 'make linux    只编译 Linux x64' 'make windows  只编译 Windows x64' 'make clean    删除构建产物'
