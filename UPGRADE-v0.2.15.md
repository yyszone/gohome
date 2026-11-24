# GoHome v0.2.15 升级说明

本版本修复外部 App 开关状态同步、电量空值展示和登录页面默认凭据提示。

## 升级到现有 WSL / Linux Git 工作区

建议在你原来的 Git 项目目录升级，这样会保留 `.git` 历史（Makefile 会从 Git 提交数生成版本号）、`data/` 设备数据库和登录状态密钥。

```bash
cd ~/gohome

# 先备份设备和账号数据
cp -a data "data.backup-$(date +%Y%m%d-%H%M%S)"

# 解压到临时目录，再覆盖源码（不会覆盖现有 data/ 和 .git/）
mkdir -p /tmp/GoHome-v0.2.15
unzip -q ~/Downloads/GoHome-v0.2.15.zip -d /tmp/GoHome-v0.2.15
cp -a /tmp/GoHome-v0.2.15/GoHome-v0.2.15/. ~/gohome/

# 检查并构建
cd ~/gohome
go test ./...
make
```

## 本版本行为

- 后端启动时先执行一次状态刷新，之后目标间隔约 30 秒查询品牌云端；页面本身仍每 15 秒读取 GoHome 本地数据。
- eWeLink 在线设备会在周期刷新中强制请求最新设备状态接口，不再只信任设备列表的缓存 params。
- 小米从设备列表和在线设备 MIoT 属性重新读取状态。只更新本地已导入设备，不自动新增或恢复隐藏设备。
- 云端没有返回电量数值时，不显示空白的“今日用电 / 本周用电 — kWh”占位；真实的 0.000 kWh 仍会显示。
- 登录页账号和密码输入框默认留空，不显示默认凭据提示。服务端认证和长期 Cookie 机制保持不变。

## 注意

- 该轮询属于近实时云端轮询，不是设备事件推送；实际延迟取决于设备数量、云端限流和网络响应，目标刷新周期约 30 秒。
- 必须保留 `data/gohome.json`、`data/gohome.vault.json`、`data/gohome.vault.key`、`data/gohome.session.key`。不要只复制数据库或重新生成密钥。
- 源码 ZIP 不带 `.git`、`data` 和编译产物。覆盖在原 Git 工作区后再 `make`，可继续使用真实 Git 提交数、哈希和提交说明。
