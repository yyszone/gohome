# GoHome v0.2.11

GoHome 是一个轻量级 Go 家庭设备管理服务，单进程运行，静态 Web 资源内嵌在可执行文件中。当前支持小米 / 米家云端 MIoT 与 eWeLink 普通账号，提供设备同步、状态读取、规格驱动控制、设备 API、日志和 mDNS 发现扩展。

本项目使用 Go + Makefile 编译，不使用 Docker、Node.js，也不需要前端构建。

## v0.2.11 更新

- 小米 MIoT 控制：根据型号规格发现真正可写属性和可调用动作。插座电源开关使用 `prop/set`；扫地机器人、摄像头等设备只有在规格公开对应动作/属性时才显示相应控制。
- 控制状态同步：非标准命名的电源属性（如 `relay_state`）在读取到布尔值后也会更新设备开关状态，避免有值却不显示开关状态。
- 设备 API：`GET /api/v1/devices/{id}` 返回设备详情、可用的 `SIID/PIID/AIID` 和参数规格。设备行悬浮或键盘聚焦时显示“复制 API”，触屏设备始终显示。
- 远程控制：设备列表展示规格允许的开关、枚举属性和动作按钮；动作输入参数按 MIoT 规格提示用户，不猜测参数编号。
- API 响应校验：小米属性写入和动作响应必须带有 `code` 与 `result`，不再将缺少结果的响应误报为成功。
- 工程结构：入口迁移到 `cmd/gohome`，Web 与服务端编排在 `internal/app`；品牌协议模块在 `internal/providers/{xiaomi,ewelink,esp32}`；公共功能按 `internal/features` 分组。
- 一条 `make`：先运行测试与 `go vet`，再同时交叉编译 Linux amd64 和 Windows amd64。
- 设备管理 UI：MIoT 控件较多的摄像头、音箱、扫地机器人等设备会单独显示控制卡片；常用开关、模式、音量、清扫/回充等快捷项放在卡片顶部，其余设置和动作折叠到“更多设置与动作”，过滤重启、删除、格式化、解绑等危险或容易误触的动作。
- 用电统计：电量面板始终显示“今日用电”和“本周用电”两个周期；已配置型号调用小米 `/v2/user/statistics`，读取 `stat_day_v3` 或旧版 `stat_day` 日记录，按本机时区汇总今日和本周（周一至当前）。
- 小米统计键按型号白名单配置（例如 `cuco.plug.cp2d`、`cuco.plug.cp4/cp4am/cp4m`、`chuangmi.plug.212a01`、`iot.plug.jdls1`、`zimi.plug.zncz01`、部分 `lumi` / `qmi` / `cuco` 插座）；未知型号不猜统计 key 和单位，数值显示 `—`。
- eWeLink：解析设备状态中可用的 `dayKwh`、`weekKwh`、`monthKwh`、`yearKwh` 字段，并按 0.01 kWh 单位换算；即使已有实时功率，如果缺少日/周累计值，也会再查询一次状态接口。
- 小米认证流程没有修改；eWeLink 现有普通账号登录与控制流程保留。

> 设备控制能力以具体设备型号、固件和小米 MIoT 规格中的 `write` 权限 / `actions` 为准。没有公开可写属性或动作的设备会保持只读，不会用错误的通用命令冒充控制成功。摄像头视频预览、厂商私有云台协议等不在通用 MIoT 控制的保证范围内。

## 项目结构

```text
.
├── cmd/
│   └── gohome/
│       └── main.go              # 程序入口
├── internal/
│   ├── app/                     # HTTP 路由、账号协调、Web、前端资源
│   │   └── web/                 # HTML / CSS / JavaScript（go:embed）
│   ├── integration/             # 品牌驱动统一接口
│   ├── providers/
│   │   ├── xiaomi/              # MIoT 规格发现、控制参数与响应校验
│   │   ├── ewelink/             # eWeLink 单路/多路控制规则
│   │   ├── esp32/               # ESP32 驱动接口预留
│   │   └── registry/            # 驱动注册表
│   └── features/
│       ├── devices/             # 品牌与设备分类
│       ├── storage/             # 原子文件操作
│       ├── discovery/           # mDNS 发现
│       └── logging/             # 敏感日志脱敏
├── scripts/                     # Linux / Windows 启动脚本
├── Makefile                     # 默认双平台编译
├── go.mod
├── README.md
└── THIRD_PARTY_NOTICES.md
```

说明：小米登录、验证码和 STS 会话编排仍位于 `internal/app` 的 `providers_xiaomi_auth.go`。这是为了保持当前已经验证可用的登录流程不变；小米 MIoT 控制能力已经独立在 `internal/providers/xiaomi`。eWeLink 与 ESP32 也有独立协议目录。

## 环境要求

- Go 1.23 或更高版本
- GNU Make（Windows 可通过 MSYS2 / Git Bash 等提供 make；也可以直接运行 `go build`）
- Linux NAS 推荐在 NAS 本机编译，避免 CGO 或 CPU 架构不匹配

## 编译

### 默认：一次编译 Linux + Windows

```bash
make
```

默认目标会依次执行：

1. `go test ./...`
2. `go vet ./...`
3. 编译 Linux amd64
4. 编译 Windows amd64

输出：

```text
dist/gohome-linux-amd64
dist/gohome-windows-amd64.exe
```

### 其他命令

```bash
make build     # 编译当前操作系统架构到 build/
make run       # 编译并运行当前平台
make dev       # go run ./cmd/gohome
make test      # 单元测试
make vet       # 静态检查
make release   # 与默认 make 相同，测试后编译两个平台
make linux     # 只编译 Linux amd64
make windows   # 只编译 Windows amd64
make clean     # 删除 build/ 和 dist/
```

## 运行

Linux：

```bash
chmod +x dist/gohome-linux-amd64
GOHOME_ADDR="0.0.0.0:17880" \
GOHOME_USER="admin" \
GOHOME_PASSWORD='请替换为自己的强密码' \
./dist/gohome-linux-amd64
```

Windows PowerShell：

```powershell
$env:GOHOME_ADDR = "0.0.0.0:17880"
$env:GOHOME_USER = "admin"
$env:GOHOME_PASSWORD = "请替换为自己的强密码"
.\dist\gohome-windows-amd64.exe
```

浏览器访问 `http://NAS_IP:17880`。

也可以使用 `scripts/start-linux.sh` 或 `scripts/start-windows.bat` 从项目目录启动。

## 数据与登录状态

首次运行自动创建 `data/`。主要文件包括：

```text
data/
├── gohome.json          # 设备、账号元数据、状态和日志
├── gohome.vault.json    # 加密后的品牌凭证
└── gohome.vault.key     # 本机 AES-GCM 加密密钥
```

升级时请停止旧程序并完整备份、复制整个 `data/` 目录。必须保留 `gohome.vault.json` 和 `gohome.vault.key`，否则旧凭证无法解密。不要把 `data/` 提交到 Git 或公开分享。

本地登录状态可以持久保存，但小米 / eWeLink 云端令牌受服务端有效期和安全策略约束，不承诺永久有效。当前版本不改动小米账号登录代码。通常升级程序和保留 `data/` 后不需要重新登录；云端令牌到期、账号改密或主动撤销授权时仍可能要求重新授权。

## 设备 API

如果设置了 `GOHOME_USER` 和 `GOHOME_PASSWORD`，API 使用 HTTP Basic Auth。示例将地址、用户名和密码替换为自己的值。

查询设备列表：

```bash
curl -u 'admin:你的密码' 'http://NAS_IP:17880/api/v1/devices'
```

查询单台设备及控制能力：

```bash
curl -u 'admin:你的密码' 'http://NAS_IP:17880/api/v1/devices/DEVICE_ID'
```

通用开关命令（小米 / eWeLink 支持的开关设备）：

```bash
curl -X POST -u 'admin:你的密码' \
  'http://NAS_IP:17880/api/v1/devices/DEVICE_ID/command' \
  -H 'Content-Type: application/json' \
  -d '{"command":"turn_on"}'
```

可用命令：`turn_on`、`turn_off`、`toggle`。多路 eWeLink 设备还需传 `channel`，通道编号以设备 API 返回值为准。

写入小米 MIoT 可写属性：

```bash
curl -X POST -u 'admin:你的密码' \
  'http://NAS_IP:17880/api/v1/devices/DEVICE_ID/property' \
  -H 'Content-Type: application/json' \
  -d '{"siid":2,"piid":1,"value":true}'
```

执行小米 MIoT 动作：

```bash
curl -X POST -u 'admin:你的密码' \
  'http://NAS_IP:17880/api/v1/devices/DEVICE_ID/action' \
  -H 'Content-Type: application/json' \
  -d '{"siid":4,"aiid":1,"params":[]}'
```

上面的 `SIID/PIID/AIID` 仅为示例，必须使用 `GET /api/v1/devices/{id}` 中当前设备实际发现的能力值和输入参数。设备管理页的“复制 API”会按每台设备生成对应示例，不会包含云端 Token 或登录密码。

完整 API 描述：`GET /api/v1/openapi.json`。健康检查：`GET /api/health`。

## 今日 / 本周用电统计

- 小米：通过型号专用统计键请求 `/app/v2/user/statistics` 的日统计记录；把当天多通道记录求和得到今日 kWh，把本周（周一 00:00 到当前）日记录求和得到本周 kWh。型号统计 key 和单位换算有差异，当前只对已知型号表启用；未知型号保持 `—`，不会把实时 W 值换算成日用电。
- eWeLink：设备状态若提供 `dayKwh` / `weekKwh` 等周期计量字段，则按 0.01 kWh 换算显示。并非所有设备固件或账号云接口都会返回周期数据；缺失时明确显示 `—`。
- 日/周统计使用运行 GoHome 的主机本地时区；如需中国自然日统计，请将 NAS 时区设为 `Asia/Shanghai`。
- 小米统计服务可能返回 `sds throttle`，此时本次同步不更新历史用电，稍后再次同步即可；不要频繁重复请求。

## 品牌支持边界

- **小米 / 米家**：沿用现有账号密码、短信 / 邮箱二次验证与服务令牌流程；设备状态和控制来自该设备 MIoT 规格。属性必须有 `write` 权限，动作必须存在于规格中。
- **易微联 / eWeLink**：普通账号登录和现有云端设备开关 / 多通道控制保留。
- **ESP32**：目前提供驱动接口目录，具体 HTTP / MQTT / 局域网协议适配器需要按固件功能实现；目录存在不代表所有 ESP32 设备已自动支持。

## 安全建议

- 不要将 GoHome 管理端口直接暴露到公网；远程访问请使用 VPN 或可信反向代理并启用强密码和 HTTPS。
- 不要分享 `data/`、日志中的认证上下文、Cookie 或云端 Token。
- 远程控制 API 有实际设备副作用；调用前确认设备 ID、属性编号和动作参数。

## 许可证与第三方声明

请参阅 `THIRD_PARTY_NOTICES.md`。小米和 eWeLink 的云端接口属于厂商服务，接口行为可能随服务端版本变化。
