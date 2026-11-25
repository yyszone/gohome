# GoHome v0.2.26 测试报告

## 测试项目

- `go test -count=1 ./...`
- `go test -race -count=1 ./...`
- `go vet ./...`
- `node --check internal/app/web/app.js`
- `sh -n scripts/service-start.sh`
- `sh -n scripts/install-systemd.sh`
- Makefile dry-run 检查
- Linux amd64、Linux ARMv7、Linux ARM64、Windows amd64 交叉编译
- ZIP 完整性和运行数据/密钥排除检查
- `systemd-analyze verify` 检查实际路径展开后的 unit
- 架构启动脚本 stub 测试（aarch64/64、aarch64/32、armv7l/32）
- Linux amd64 服务 smoke test：HTTP health 返回成功，SIGTERM 后记录优雅停止并退出 0

## 部署实现检查

- ARMv7 使用 `GOOS=linux GOARCH=arm GOARM=7`。
- ARM64 使用 `GOOS=linux GOARCH=arm64`。
- 两种 ARM 产物均使用 `CGO_ENABLED=0`，不依赖目标机 C 运行库。
- systemd 启动脚本结合 `uname -m` 和 `getconf LONG_BIT` 选择二进制，兼容 AArch64 内核 + ARM32 用户空间，并通过 `exec` 替换 shell 进程。
- unit 将 `WorkingDirectory` 固定到项目目录，`GOHOME_DATA_DIR` 固定到项目下 `data/`。
- 服务仅在异常退出时自动重启，不设置 CPU/内存限额；日志写入 journal。
- SIGTERM 和 Ctrl+C 使用统一优雅关闭流程，状态轮询 Context 会在 HTTP 服务关闭前取消。
- `file` 识别 ARMv7 为 32-bit ARM ELF/EABI5，ARM64 为 64-bit ARM aarch64 ELF；两者均为静态链接。

## 验证边界

交叉编译与本地测试不能代替魔百盒实机验证。本报告不声称已经在用户的 Armbian 盒子上启动或验证网络设备；部署后应根据 `uname -m` 和 `journalctl -u gohome` 确认实际运行架构与服务状态。
