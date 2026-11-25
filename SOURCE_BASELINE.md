# 源码基线说明

GoHome v0.2.24 基于当前可取得的完整 GoHome v0.2.23 源码 ZIP 修改。v0.2.23 ZIP 本身基于归档中的 v0.2.22 源码树。

历史归档存在来源限制：更早的 v0.2.17 / v0.2.18 快照曾基于当时可取得的 v0.2.15 源码树；未取得完整 v0.2.16 源码 ZIP。因此，如果用户本机 Git 工作区有未进入 v0.2.22 / v0.2.23 归档的额外实现（例如独立开发的 eWeLink Cloud4 WebSocket 或其他驱动），升级前必须先比较并合并，不要直接覆盖生产目录。

## v0.2.24 修改范围

- `internal/app/page_activity.go`：后端状态轮询调度从每秒 ticker 改为页面租约事件唤醒。没有活跃页面时阻塞等待；页面租约变更唤醒调度；轮询进行中只等待页面事件或最早租约到期。
- `internal/app/integration_manager.go`：新增调度器 wake channel，并在页面租约更新、轮询结束时唤醒调度器。
- `internal/app/providers_ewelink.go`：普通账号/设备同步时通过 `/v2/family` 把 `family.roomid` 映射为房间名称；强制实时状态刷新不会调用该接口。
- `internal/app/providers_xiaomi_miot.go`：普通设备同步时通过 `v2/homeroom/gethome_merged` 把家庭房间 DID 映射为名称；实时状态刷新跳过该元数据请求。
- `internal/app/integration_manager.go`：房间查询未返回名称时保留旧房间名，避免“未分区”覆盖已有位置。
- `internal/app/main.go`：Account 增加 `DisplayName`；新增 `PATCH /api/accounts/{id}`，仅持久化 UI 别名，不改变登录标识或 Vault。
- `internal/app/web/app.js` / `app.css`：品牌接入账号卡片显示别名并增加重命名入口。
- `internal/app/main_test.go`：增加事件唤醒、房间解析/保留、账号重命名不触碰登录身份等回归测试。

## 保留项

- 保留原小米和 eWeLink 登录、令牌和 Vault 格式。
- 保留 eWeLink 和小米设备控制代码。
- 保留 `.gitignore` 与 `data/.gitkeep`。
- ZIP 不含 `.git`、真实运行数据、Vault/Session 密钥、二进制文件。
