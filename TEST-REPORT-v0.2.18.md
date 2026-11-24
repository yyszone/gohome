# GoHome v0.2.18 测试记录

本次没有生成 Linux/Windows 可执行文件；仅执行源码测试与静态检查。以下命令均实际执行，Go 测试使用 `-count=1` 禁用测试缓存。

| 检查 | 结果 |
|---|---|
| `gofmt` 全量检查 | 通过，无未格式化文件 |
| `node --check internal/app/web/app.js` | 通过 |
| `go test -count=1 ./...` | 通过 |
| `go test -race -count=1 ./...` | 通过 |
| `go vet ./...` | 通过 |
| eWeLink 设备列表旧 ON、状态接口新 OFF 的模拟测试 | 通过 |
| MIoT relay_state / property:on 映射测试 | 通过 |
| MIoT OFF 值与 0/1、on/off 值解析测试 | 通过 |
| MIoT 多设备批量响应按 DID 隔离，缺少 DID 不误分配 | 通过 |
| 在线设备没有新开关读数时清除陈旧 Power 的测试 | 通过 |
| live API revision / HTTP 204 / 外部 OFF 更新测试 | 通过 |
| 页面租约、多标签页和过期停止轮询测试 | 通过 |
| 控制命令后只刷新 live delta 的前端代码检查 | 通过（语法与源码检查） |

以上均为本地测试服务器和模拟设备响应，不代表已连接用户的真实小米/eWeLink 账号或实体设备实测。实际云端可用性、区域、型号支持情况仍需在用户 NAS 上验证。
