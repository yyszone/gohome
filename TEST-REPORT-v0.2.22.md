# GoHome v0.2.22 测试报告

## 已执行并通过

- `go test -count=1 ./...`
- `go test -race -count=1 ./...`
- `go vet ./...`
- `node --check internal/app/web/app.js`
- `make -n`（仅检查 Makefile 默认目标，不执行编译）
- ZIP 内容核查：包含根目录 `.gitignore` 和 `data/.gitkeep`；不包含 `.git`、真实 `data`、Vault/Session 密钥和二进制。

## 本版新增回归测试

- `TestImmediateXiaomiPowerUpdateKeepsM3IndicatorIndependent`：模拟 Xiaomi M3 主继电器从 ON 改成 OFF，确认 `Device.Power` 和唯一的“开关状态”同步为 OFF，同时保留 `indicator_light` 与温度属性，不覆盖指示灯状态。

## 未进行的测试

- 未连接真实小米账号或 `chuangmi.plug.m3` 实体插座执行云端命令。
- 未验证真实小米云端 `prop/set` 的响应延迟和后台 `prop/get` 最终读回时间。
- 按要求未执行 `make`，未生成 Linux / Windows 二进制。

小米控制流程的行为是：`prop/set` 返回成功后立即更新本地状态并响应页面；后台独立 Context 延迟读取同一个主继电器属性，成功时以实际读数校正。若读回暂时失败，日志会记录原因，保留已被云端接受的命令目标状态，页面活跃时的后续状态轮询仍可校准。
