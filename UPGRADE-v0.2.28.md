# GoHome v0.2.28 升级说明

v0.2.28 在 v0.2.27 完整源码归档上增加设备语义修正、管理员认证管理、摄像头 HTTP 视频源入口、移动端适配和远程部署脚本。该 ZIP 不依赖旧的 `GoHome-device-auth-deploy.patch`，解压后即为完整源码树。

## 更新内容

### 小米环境传感器

- MIoT 温湿度属性解析同时考虑属性名称、说明、服务名称/说明/Type 和单位。
- 支持 `currentTemperature`、`value`、`current`、`measurement` 等部分型号常见的通用属性命名；摄氏度/华氏度归类为温度，百分比归类为湿度。
- 修正 `currentTemperature` 中的 `current` 子串被误判为电流的情况；真实电流仍按安培单位和电气服务元数据处理。

### 扫地机器人

- 按类别、型号和名称识别 Roborock / Rockrobo / Vacuum 类设备。
- MIoT 的数组/对象状态不再被认为是布尔电源值；扫地机器人不生成通用电源开关，也不计入“开启设备”。
- 旧数据库中历史遗留的伪电源状态和“开关状态”属性会在设备快照中被过滤；扫地、拖地、返回充电座等可用 MIoT 动作仍保留。

### 摄像头网页预览

- 摄像头设备卡片和列表增加“视频源”配置入口，保存地址到设备记录的 `cameraUrl` 字段。
- 浏览器可直接处理 MJPEG、MP4、WebM；`.m3u8` 使用原生 HLS 播放能力，具体取决于浏览器。
- 仅接受 HTTP/HTTPS URL，拒绝 RTSP、`javascript:` 和带 URL 用户名密码的地址。
- 摄像头私有云/P2P 视频流需要单独的视频网关先转换成 HTTP 流；本版本不声称能直接播放所有品牌摄像头。

### 管理员密码和登录 Cookie

- 首次启动生成独立随机管理员密码（48 位十六进制），保存到 `data/gohome.admin.password`，文件权限为 `0600`；不再使用所有用户共用的 `123456`。
- 首次登录可在服务端执行 `cat data/gohome.admin.password` 读取密码。首次启动日志也会打印该文件路径，不会打印密码本身。
- 登录 Cookie 保持 10 年有效期；已认证页面访问时滚动刷新持久 Cookie。
- 页面右上角增加“修改密码”，新密码要求 12–128 个字符。修改成功会轮换 Session 签名密钥，使其他浏览器旧 Cookie 立即失效，当前浏览器自动取得新 Cookie。
- 新增密码原子保存、文件权限、重启后密码持续有效和旧 Cookie 失效测试。

### 设备页面响应式优化

- 优化移动设备上的设备列表、控制卡片、视频预览和顶部操作区布局。
- 保留桌面端现有布局和 GoHome 原有主题资源。

### 一条命令远程部署

默认使用现有 `scripts/service-start.sh` 对应的二进制文件名，部署不会创建服务启动脚本不会读取的独立文件：

```bash
make deploy DEPLOY_HOST=root@192.168.31.63 DEPLOY_ARCH=arm64
```

可选架构：`arm64`、`armv7`、`amd64`。可覆盖部署目录和服务名：

```bash
make deploy DEPLOY_HOST=root@192.168.31.63 DEPLOY_ARCH=arm64 DEPLOY_PATH=/root/gohome DEPLOY_SERVICE=gohome
```

脚本会先运行测试/静态检查，再构建所选架构；连接远程主机校验 `uname -m` 和用户空间位数；上传隐藏临时文件并在同一文件系统内原子替换目标二进制。重启服务并确认 `systemctl is-active` 成功后清除回滚文件；若启动失败，则恢复原二进制并尝试重新启动旧版本。`data/`、Vault、Session 密钥、管理员密码和配置文件不参与替换。

前置条件：本机已配置 SSH 密钥登录、安装 `ssh`/`scp`，远程主机使用 systemd，账户有部署目录写权限和服务管理权限。默认目录 `/root/gohome`、服务名 `gohome`；路径不能包含空格或 Shell 特殊字符。

## 升级保留数据

若当前安装已经在运行，只需把源码包解压到开发目录，确认 `DEPLOY_PATH` 与现有 systemd 项目目录一致，再执行 `make deploy`。部署脚本只替换对应架构二进制，不删除或覆盖远程 `data/`。升级前仍建议备份：

```bash
cd /root/gohome
tar -czf ../gohome-data-backup-$(date +%Y%m%d-%H%M%S).tar.gz data
```

注意：本源码 ZIP 是以归档中的 v0.2.27 快照为基线，不可能包含当前未上传到该归档的私人本地修改。第一次替换生产二进制前，请确认此版本包含你需要的本地功能，尤其是未归档的品牌协议扩展。完整基线说明见 `SOURCE_BASELINE.md`。
