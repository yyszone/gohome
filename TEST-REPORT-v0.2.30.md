# GoHome v0.2.30 测试记录

测试日期：2026-11-26

## 自动化验证

| 检查 | 结果 | 说明 |
|---|---|---|
| `go test ./...` | 通过 | 全部 Go 单元测试通过 |
| `go test -race ./...` | 通过 | 全部 Go 测试通过竞态检测 |
| `go vet ./...` | 通过 | 未发现静态分析问题 |
| `node --check internal/app/web/app.js` | 通过 | 前端 JS 语法无误 |
| Shell 脚本 `sh -n` | 通过 | `scripts/` 和 `deploy/` 下脚本逐个检查 |
| `make armbian` | 通过 | 实际交叉编译 ARMv7 EABI5 32 位和 AArch64 64 位静态 ELF |
| ZIP 完整性 | 通过 | `unzip -t` 无损坏；解压后的 Go 测试重新通过 |

## 本版本新增回归测试

- 同一 MIoT model 返回多个 type 版本时，保留全部版本并按数字版本号降序选择。
- prop/get 响应兼容数字/字符串 SIID/PIID；显式坐标错误时可回退到 `iid` 中的服务/属性坐标。
- RB03 路由器属性可显示为下载速率等网络遥测，不能写入 `Device.Power`。
- C103 扫地机器人使用状态枚举和电池属性，不把字符串 `vacuum.on` 当作电源开关。
- X08C 触屏音箱可读取音量、静音和睡眠模式；只有 notify 的播放状态不进入读取请求。
- 没有任何 `access: read` 属性的型号不会构造空 prop/get 请求，也不应计为云端状态同步失败。

## 尚未完成的真实环境验证

- 未连接用户真实小米账号执行线上 prop/get。
- 未直接在 RB03、RA67、R3G、LV1 路由器固件上验证属性响应。
- 未直接在 C103 扫拖机器人和 X08C 音箱上验证实时值。
- 官方 HA Xiaomi Home v2 API 需要 OAuth2 Bearer token；本次没有将旧 `serviceToken/ssecurity` 冒充 Bearer token，也没有新增 OAuth2 登录。
- 未连接用户 NAS 执行真实 systemd 远程部署。

因此，本测试报告只确认本地逻辑、合成 MIoT fixture、静态检查和交叉编译通过，不代表用户云端设备已经实测。
