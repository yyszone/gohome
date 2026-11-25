# GoHome v0.2.19 测试记录

| 检查 | 结果 |
|---|---|
| `go test ./...` | 通过 |
| `go test -race ./...` | 通过 |
| `go vet ./...` | 通过 |
| JavaScript 语法检查 (`node --check internal/app/web/app.js`) | 通过 |
| `.gitignore` 存在于源码根目录 | 已确认 |
| ZIP 完整性 | 打包后执行 |

新增测试覆盖：
- 小米 property/get payload 仅包含单一 DID，多个属性仍合并，重复 SIID/PIID 去重。
- 单设备查询响应缺失 DID 时，按该请求对应的 SIID/PIID 正确更新 OFF 状态。
- 响应显式包含其他 DID 时拒绝应用，避免跨设备状态污染。
- MIoT 型号索引大小写不一致时可以匹配。

说明：测试使用本地模拟响应，没有使用用户真实小米账号或设备做云端实测。协议读取逻辑经过 HA 社区旧版 `xiaomiio` 请求实现及小米 MIoT Spec 文档对照，但是否被具体账号区域、设备型号或云端风控接受，仍需 NAS 实机日志验证。


本次未运行 `make`，未生成 Linux / Windows 二进制；由用户在原 Git 工作区自行执行 `make`。
