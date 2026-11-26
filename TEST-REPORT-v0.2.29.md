# GoHome v0.2.29 测试记录

测试日期：2026-11-26

## 自动化验证

| 检查 | 结果 | 说明 |
|---|---|---|
| `go test ./...` | 通过 | 全部 Go 单元测试通过 |
| `go test -race ./...` | 通过 | 全部 Go 测试通过竞态检测 |
| `go vet ./...` | 通过 | 未发现静态分析问题 |
| `node --check internal/app/web/app.js` | 通过 | 前端 JS 语法无误 |
| `sh -n`（逐个检查 4 个脚本） | 通过 | 部署、服务启动、systemd 安装、Linux 启动脚本均通过语法检查 |
| 部署脚本 SSH 复用模拟测试 | 通过 | Mock SSH/SCP 记录到 1 个 ControlMaster；预检、SCP 和部署使用同一 ControlPath；自定义目录 `/opt/services/gohome` 传递正确 |
| `make armbian` | 通过 | 实际构建 ARMv7 EABI5 32 位和 AArch64 64 位静态 ELF |

## 本版本重点回归

- 修改管理员密码为 `123456` 后可成功登录，并在重新创建 App 后继续生效。
- 5 位密码（如 `12345`）必须返回 HTTP 400。
- 既有 48 位随机密码首次生成和 `0600` 文件权限保持不变。
- 部署脚本只启动一个 SSH ControlMaster，架构预检、远程目录创建、SCP 和最后部署命令均复用相同 ControlPath。
- `DEPLOY_PATH` 默认为 `/root/gohome`，指定 `/opt/services/gohome` 时必须向该目录创建临时文件并原子替换对应架构二进制。
- 部署失败时仍尝试恢复旧二进制；`data/` 不参与覆盖。

SSH 连接复用通过 Mock SSH/SCP 验证调用结构和共享 ControlPath；真实 SSH 密码提示、远程 systemd 重启和用户 NAS 联调仍需在实际部署环境验证，本报告不将本地模拟称为真实远程部署。
