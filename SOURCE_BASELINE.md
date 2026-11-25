# 源码基线说明

GoHome v0.2.23 基于当前归档中的完整 GoHome v0.2.22 源码继续修改。本包包含 v0.2.22 已归档的所有文件，以及本次列出的状态确认延迟与并发保护变更。

历史归档仍存在更早的来源限制：v0.2.20 基于可取得的 v0.2.19 源码；更早的 v0.2.18 / v0.2.17 快照曾基于可取得的 v0.2.15 源码树，期间没有取得完整的 v0.2.16 源码 ZIP。因此如果你的本地 Git 工作区中有未进入 v0.2.22 ZIP 的额外修改（例如独立开发的 eWeLink Cloud4 WebSocket 或其他驱动），请先比较后合并，不要盲目覆盖生产目录。

## v0.2.23 修改范围

- `internal/app/main.go`：移除小米命令成功后 700ms 的单次状态读回；小米主开关和主电源属性写入在 `prop/set` 成功后立即保存乐观状态并返回。记录每台设备最近一次主电源命令时间；状态存储层拒绝应用在该命令之前启动、之后才返回的旧轮询快照。
- `internal/app/provider_state_poller.go`：将每轮品牌采集的开始时间传递给存储层；小米状态读取失败时，仅当该轮请求开始时间晚于最近命令时间，才允许清除旧电源状态。旧请求的取消/超时不再清除刚刚点击产生的乐观状态。
- `internal/app/main_test.go`：新增 `TestOlderXiaomiPollCannotRevertOptimisticCommand`，覆盖旧轮询晚到返回 ON、旧轮询失败清理、以及命令之后的新轮询纠正状态三种情况。
- `README.md`、`UPGRADE-v0.2.23.md`、`TEST-REPORT-v0.2.23.md`：补充本版行为和验证说明。

## 没有修改的部分

- 不修改小米登录、验证码、serviceToken、ssecurity、RC4 签名和 Vault 数据结构。
- 不修改 eWeLink 登录与控制协议、前端 AJAX 方式或 10 秒页面活跃品牌轮询节奏。
- 保留 v0.2.22 的 MIoT 服务级开关映射、M3 指示灯隔离、设备控制页面、`.gitignore` 和 `data/.gitkeep`。

ZIP 不含 `.git`、真实 `data/`、Vault/Session 密钥和二进制。请保留原 Git 工作区，以便 Makefile 使用原提交总数、短哈希和最新提交说明生成动态版本。
