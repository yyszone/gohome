# GoHome v0.2.21 测试报告

本次测试聚焦小米 `chuangmi.plug.m3` 主继电器/指示灯状态一致性、旧数据迁移与页面租约请求生命周期。

| 测试 | 结果 |
|---|---|
| `go test -count=1 ./...` | 通过 |
| `go test -race -count=1 ./...` | 通过 |
| `go vet ./...` | 通过 |
| `node --check internal/app/web/app.js` | 通过 |
| `make -n` 默认构建流程检查 | 通过（仅 dry-run，未编译） |
| M3 主继电器 OFF、指示灯 ON，任意返回顺序 | 通过 |
| M3 老数据含两条“开关状态”时启动迁移 | 通过 |
| M3 真实读回 OFF 后属性/摘要/按钮同为关闭 | 通过 |
| MIoT bool 与数值 0/1 规范化 | 通过 |
| M3 迁移保留米家账号及 Vault/Session 文件 | 通过 |
| 页面租约释放不取消已在途云端请求 | 通过 |
| ZIP 完整性、`.gitignore`、`data/.gitkeep` 和凭证排除检查 | 打包后通过 |

没有执行 Linux / Windows 二进制编译（按用户要求由用户本机 `make` 编译），也没有在本环境连接真实小米账号与 `chuangmi.plug.m3` 插座实测。
