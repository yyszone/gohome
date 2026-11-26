# GoHome v0.2.31 测试报告

## 自动化测试

- `go test ./...`：通过。
- `go test -race ./...`：通过。
- `go vet ./...`：通过。
- `node --check internal/app/web/app.js`：通过（前端无功能改动，仅版本缓存标识沿用）。
- `sh -n scripts/deploy.sh scripts/install-systemd.sh scripts/service-start.sh scripts/start-linux.sh`：逐个脚本语法检查通过。
- `GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build ./cmd/gohome`：交叉编译通过。
- `GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 go build ./cmd/gohome`：交叉编译通过。

## 新增回归覆盖

- 未收录 MIoT 型号显示为设备规格/能力限制。
- 日志明确显示账号连接正常，不暗示登录失败或设备离线。
- 多台设备问题正确计数，超过展示上限时提示未展开数量。
- 无设备状态问题时不生成设备级警告。
- 一台设备异常不会覆盖或合并成账号级云端请求异常；两类日志使用独立限频键。

## 现场验证边界

自动化测试不等同于真实小米云账号或手环现场联调。需要部署后观察设备级日志是否符合预期；本次没有修改小米 API、认证或 MIoT 规格数据源。
