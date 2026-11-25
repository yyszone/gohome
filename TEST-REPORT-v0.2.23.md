# GoHome v0.2.23 测试报告

## 自动化检查

- `go test -count=1 ./...`
- `go test -race -count=1 ./...`
- `go vet ./...`
- `node --check internal/app/web/app.js`
- `make -n` 默认目标检查（仅 dry-run，不编译）
- ZIP 完整性检查
- `.gitignore` 存在性检查
- 检查 ZIP 不含 `data/gohome.json`、Vault/Session 密钥和任何可执行二进制

## 新增回归测试

`TestOlderXiaomiPollCannotRevertOptimisticCommand` 验证：

1. 初始设备 ON，成功发送 OFF 命令后，本地状态立即变为 OFF。
2. 命令之前已经启动的轮询即使晚到并返回 ON，也不能覆盖 OFF。
3. 旧轮询失败时的状态清理也不能清除 OFF。
4. 命令之后启动的新轮询允许确认真实状态；若云端新读数为 ON，则可以纠正乐观状态。

## 实机验证边界

本次为源码和模拟状态测试，没有接入用户实际小米插座验证云端传播延迟。真实确认间隔受当前页面活跃状态、后台 10 秒轮询节奏和小米云端响应时间影响，不承诺固定秒数。

未执行 `make`，没有编译 Linux/Windows 二进制。
