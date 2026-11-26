## 源码基线补充

GoHome v0.2.28 基于本目录完整 v0.2.27 源码 ZIP，保留 v0.2.27 品牌图标与主题资源，并新增管理员密码管理、MIoT 设备语义修复、摄像头 HTTP 视频源、响应式细节和原子化远程部署。该包不包含用户未归档到 v0.2.27 ZIP 的本机私有修改。

# 源码基线说明

GoHome v0.2.25 基于当前工作区中的 GoHome v0.2.24 源码修改。v0.2.24 源码基于可取得的完整 GoHome v0.2.23 源码 ZIP；v0.2.23 ZIP 本身基于归档中的 v0.2.22 源码树。

历史归档存在来源限制：更早的 v0.2.17 / v0.2.18 快照曾基于当时可取得的 v0.2.15 源码树；未取得完整 v0.2.16 源码 ZIP。因此，如果用户本机 Git 工作区有未进入 v0.2.22 / v0.2.23 归档的额外实现（例如独立开发的 eWeLink Cloud4 WebSocket 或其他驱动），升级前必须先比较并合并，不要直接覆盖生产目录。

## v0.2.27 修改范围

- `internal/app/web/index.html`：加入 SVG/PNG/ICO favicon、Apple Touch Icon、Web App Manifest 和独立主题样式表；登录页与侧边栏使用统一 GoHome Logo。
- `internal/app/web/theme.css`：新增轻量 CSS 视觉覆盖层，统一海军蓝导航、青绿色品牌主色、卡片/按钮/表单/登录页样式；不改变既有 DOM 行为和响应式断点。
- `internal/app/web/favicon.svg`、`favicon.ico`、`favicon-*.png`、`gohome-logo.png`、`gohome-icon-*.png`、`apple-touch-icon.png`、`site.webmanifest`：新增品牌静态资源，由 `//go:embed web/*` 编入单文件程序。
- `internal/app/main.go`：更新源码版本标识为 `0.2.27-source`；静态首页不缓存、带版本号的静态文件缓存 24 小时，并为 `.webmanifest` 设置标准 MIME 类型。
- `Makefile`：在没有 `.git` 的源码 ZIP 中使用 `SOURCE_VERSION=0.2.27`；处于 Git 工作区时仍按提交计数生成版本。
- 不修改 API、设备模型、设备控制、品牌登录和数据存储格式。

## v0.2.25 修改范围

- `internal/app/providers_xiaomi_miot.go`：按 HA 的 `gethome_merged` 逻辑扩展主家庭、共享家庭、设备映射和 DID 类型解析；无映射时按官方 Xiaomi Home 集成回退到 `gethome` 与 `get_dev_room_page` 分页接口；保留房间查询错误到同步期日志。
- `internal/app/main.go`：小米/eWeLink 开关控制成功、eWeLink Token 刷新失败及控制失败均进入运行日志；增加瞬时房间查询诊断字段；放宽小米同步 HTTP 超时。
- `internal/app/integration_manager.go` / `providers_xiaomi_auth.go`：小米同步后写入房间映射汇总/错误日志。
- `internal/app/web/app.js`：开关、MIoT 属性与动作操作后刷新本地运行日志。
- `internal/app/main_test.go`：增加数字 DID、共享家庭、预合并设备映射、房间分页与同步诊断日志测试。

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

## v0.2.28 修改范围

- `internal/app/admin_password.go`：每个安装首次生成 48 位随机管理员密码，写入私有 `data/gohome.admin.password`；提供修改密码和 Session 密钥轮换。
- `internal/app/auth.go` / `main.go`：使用生成的管理员密码，持久 Cookie 在认证后滚动刷新，修改密码使旧浏览器会话失效；设备 API 支持 `cameraUrl` 字段。
- `internal/app/camera.go`：校验浏览器可访问的视频 URL。
- `internal/app/device_semantics.go`：识别扫地机器人并清理旧的错误电源状态；快照输出层不污染原始数据库。
- `internal/app/providers_xiaomi_miot.go`：用服务名称/说明/类型和单位识别通用温度/湿度读数；拒绝把机器人数组状态映射成开关状态。
- `internal/app/web/index.html` / `app.js` / `app.css`：密码修改弹窗、摄像头视频源管理和浏览器播放、机器人电源开关隐藏、移动布局。
- `scripts/deploy.sh` 与 `Makefile`：按远端实际架构校验、临时上传、原子替换架构二进制、systemd 健康确认和失败回滚。
- `docs/PATCHING.md`：源码模块分工和后续 patch 操作方式。
- 新增管理员密码、摄像头、MIoT 传感器和扫地机器人回归测试。

## v0.2.28 安全文件说明

真实管理员密码和 Session 签名密钥只存在部署实例的 `data/` 目录中，不随源码 ZIP 分发。`gohome.admin.password` 与 `gohome.session.key` 均应限制为服务用户可读（0600），备份运行数据时按密钥处理，不要上传公开仓库。
