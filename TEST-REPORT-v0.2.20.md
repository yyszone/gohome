# GoHome v0.2.20 测试报告

本次测试针对小米 MIoT 同名属性映射修复。

| 测试 | 结果 |
|---|---|
| `go test -count=1 ./...` | 通过 |
| `go test -race ./...` | 通过 |
| `go vet ./...` | 通过 |
| `node --check internal/app/web/app.js` | 通过 |
| `chuangmi.plug.m3` 服务属性分类 | 通过 |
| 云端先返回 switch OFF、后返回 LED ON | 通过 |
| 云端先返回 LED ON、后返回 switch OFF | 通过 |
| indicator 状态独立显示且不覆盖 Device.Power | 通过 |
| `.gitignore` 存在且 data 密钥不打包 | 通过 |

没有执行 GoHome 可执行文件编译（按用户要求由用户本机 `make` 编译），也没有连接真实小米账号/插座进行实机验证。
