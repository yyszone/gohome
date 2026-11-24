# GoHome v0.2.17

## v0.2.17 页面活跃租约式状态同步

- 状态轮询不再随 GoHome 进程启动而常驻执行；仅当已登录的浏览器标签页可见且停留在“总览”或“设备管理”时，才请求小米 / eWeLink 云端状态。
- 浏览器每 8 秒续租，服务端租约有效期 25 秒；品牌状态刷新目标间隔 30 秒。页面隐藏、切换到其他模块或关闭时释放租约；异常退出时租约自动过期。
- 多个标签页使用独立 ID；一个标签页隐藏不会影响另一个仍处于活动状态的设备页面。
- 没有活动页面时，后台不访问品牌云端；只保留每 2 秒检查租约的轻量本地定时器。网页原有 15 秒轮询只读取本地数据库。
- 新增受登录 Cookie 保护的 `POST /api/state-sync/activity`，API 返回 `Cache-Control: no-store`。

**源码基线说明：** 当前工作区没有完整的 v0.2.16 源码 ZIP，只有其 README/升级说明。本包基于最后一份可取得的完整 v0.2.15 源码树增加页面租约逻辑，不能保证包含 v0.2.16 期间所有未提供的代码改动。若你的现有 v0.2.16 已有额外的 eWeLink Cloud4 WebSocket 等实现，请先比较源码，不要直接覆盖生产版本。详情见 `SOURCE_BASELINE.md`。

## 登录与小米短信验证优化

- 小米短信错误 `70022` 不再错误提示“无法读取验证渠道”。现在会明确指出：安全验证方式已经读取，失败发生在小米短信发送阶段；保留原始登录 CookieJar 和验证会话，不要求重新输入小米密码。
- 如果小米账号同时开放短信和邮箱验证，页面可以在当前会话中切换验证渠道；不需要重新创建登录会话。
- `70022` 表示小米验证码发送请求被拒绝，但公开资料没有给出这个错误码的唯一释义。若其他手机号能收到验证码，优先检查当前绑定安全手机号的发送额度、号码风控、运营商路由和短信退订拦截。小米官方说明同一手机号 24 小时短信上限通常为中国大陆 5 次、其他国家/地区 3 次；达到上限后需等待 24 小时。
- 删除侧栏重复的 `sidebar-foot` 版本信息，版本元信息只保留在页面底部。
- 首次访问显示 GoHome 内置登录页；初始账号固定为 `admin`，初始密码固定为 `123456`，不再读取 `GOHOME_USER` / `GOHOME_PASSWORD`。
- 登录后由服务端签发 HttpOnly、SameSite Strict 的持久 Cookie；签名密钥写入 `data/gohome.session.key`，进程重启后仍可验证旧 Cookie。
- 登录失败 5 次后对来源 IP 暂时限流；API 复制示例同步改为 Cookie 登录方式。

**安全警告：** `admin/123456` 是所有未修改安装共用的弱默认密码。GoHome 可控制插座、摄像头等家庭设备，只能放在可信局域网内使用，不要把 17880 端口直接映射到公网。Cookie 服务器端签名没有自动过期，浏览器 Cookie 设置为长期持久保存；浏览器本身可能按自身策略限制最长保留时间。丢失 `data/gohome.session.key` 会使旧 Cookie 失效，需要重新登录。

GoHome 是一个轻量级 Go 家庭设备管理服务，单进程运行，静态 Web 资源内嵌在可执行文件中。当前支持小米 / 米家云端 MIoT 与 eWeLink 普通账号，提供设备同步、状态读取、规格驱动控制、设备 API、日志和 mDNS 发现扩展。

## v0.2.15 历史变更（已由 v0.2.17 页面租约策略接管轮询时机）

- 后端新增品牌云端状态轮询：小米使用设备列表和在线设备 MIoT 属性读取，eWeLink 使用账号设备清单及可用状态接口读取；实际更新时延取决于设备数量、云端响应速度和网络状况。v0.2.17 已进一步限制为只有总览/设备管理页面活跃时才执行。
- 只更新已导入设备，不自动添加新设备、不恢复用户隐藏设备；云端请求失败时保留最后一次有效状态，并在日志中按账号节流记录。
- 电量面板只渲染真实存在的数值；缺少今日/本周值时隐藏对应指标，不再显示 `— kWh`。
- 登录页清除账号预填和默认账号/密码提示，认证逻辑与长期 Cookie 不变。


本项目使用 Go + Makefile 编译，不使用 Docker、Node.js，也不需要前端构建。本 v0.2.17 源码包不附带编译产物；已执行 `go test ./...`、`go test -race ./...`、`go vet ./...` 和前端 JavaScript 语法检查，双平台可执行文件由你在原 Git 工作区运行 `make` 生成。完整源码来源限制见 `SOURCE_BASELINE.md`。

## v0.2.14 更新

- 内置管理员登录页：`POST /api/auth/login` 校验源代码中的固定初始账号密码；首次进入先登录，API 未认证时返回 JSON 401。
- 长期登录 Cookie：`gohome_session` 为 HttpOnly、SameSite Strict，过期时间设置为 10 年；密钥 `data/gohome.session.key` 使用随机 256-bit 值创建并以 0600 权限保存。升级时必须保留该文件，浏览器登录状态才会跨重启延续。
- 登录暴力尝试保护：同一来源 IP 在 15 分钟内连续失败 5 次，将临时锁定 10 分钟。
- 小米 70022：准确区分“读取验证渠道失败”和“小米拒绝发送验证码”；同一验证会话可选短信或邮箱重试。
- 清理侧栏重复版本文字；设备复制 API 的 curl 示例改为先登录并保存 Cookie 文件，再携带 Cookie 调用设备接口。
- 小米登录、eWeLink 登录与设备驱动不改动；本次认证调整只针对 GoHome Web 管理入口。


## v0.2.13 更新

- 电量采样时机：eWeLink 品牌账号每次成功同步后立即采样一次；首次建立电量基线不需要等待下一次采样周期。
- eWeLink 统计来源更清晰：识别官方 `dailyKwh` / `monthlyKwh`（0.01 kWh 单位）、兼容部分设备的 `dayKwh` / `weekKwh` 与 `energy_day` / `energy_week`。
- 月累计差值补算：没有日累计但提供月累计的型号，会在同一个本机自然日内用连续月计数差值补充当日增量，并汇总到本周；跨自然日的差值不会硬塞给今天，避免虚构精度。
- 本机功率积分：缺少日/月计数时，优先使用相邻真实功率样本的平均功率积分；如果云端本次响应没返回功率，可在最近一次真实功率读数后的 15 分钟内短暂延用该读数估算，并明确标注“最近一次有效读数”。超过 15 分钟不再外推，避免把过期功率当成全天读数。
- 首次采样：成功同步 eWeLink 账号后立即触发一次采样；若云端状态暂时不返回计量字段，可使用 10 分钟内的同步功率快照建立一次 `0.000 kWh` 起点。这个起点代表“从首次采集开始”，不包含采集前已经消耗的电量。
- 采集诊断：状态接口失败、`params` 为空、或没有返回有效功率/电量时，日志会列出识别到的计量字段名（不记录字段值、Token 或密码），15 分钟节流并脱敏。
- 用电面板：今日/本周读数下方显示数据来源和限制；建立采样基线后会显示从采集时刻开始的累计值，不再无说明地一直显示 `—`。

## v0.2.12 更新

- 用电统计修正：解析 eWeLink 官方状态字段 `dailyKwh` / `monthlyKwh`（单位 0.01 kWh），并兼容部分设备的 `dayKwh` / `weekKwh`。
- 今日 / 本周本机估算：云端不提供周期累计值时，GoHome 每 5 分钟采集真实功率，按相邻采样的平均功率积分；首次有效采样建立 `0.000 kWh` 基线，明确标记“从首次采集开始”。不推算升级前的历史耗电，也不跨超过 15 分钟的采样中断补数。
- 计量采集日志：凭证读取、Token 刷新、设备状态请求、缺少功率/电量字段或保存失败会进入日志；同一设备 15 分钟内最多记录一次，敏感内容脱敏；缺少字段时只记录字段名，不记录参数值。
- 同步兼容：新云端响应缺少今日 / 本周数据时保留本地历史值，不会在重新同步后把已累计的数值清空。
- 总览卡片：统一设备标题、品牌/房间、在线状态、摘要、关键属性和能源摘要；每张卡片的设备控制统一折叠，避免复杂 MIoT 选项挤乱总览。
- 设备悬浮工具栏：设备列表与总览卡片统一使用悬浮工具栏，包含“复制 API”和删除图标；桌面端悬浮 / 键盘聚焦显示，触屏设备常驻显示（触屏没有 hover）。
- 永久隐藏：删除设备会写入本地隐藏记录，普通品牌同步不会重新导入；删除品牌账号后重新登录会清除此账号的隐藏记录，重新同步即可恢复。
- 构建版本：`make` 从 Git 读取提交总数、短哈希、最近提交内容和构建日期，通过 `-ldflags` 注入程序；页面页脚悬浮显示这些元信息。版本号形如 `0.2.<Git提交总数>`。
- 本发布 ZIP 只包含源码和脚本，不预先编译二进制；运行 `make` 时由用户本机完成测试、静态检查和 Linux / Windows amd64 构建。

## v0.2.11 更新

- 小米 MIoT 控制：根据型号规格发现真正可写属性和可调用动作。插座电源开关使用 `prop/set`；扫地机器人、摄像头等设备只有在规格公开对应动作/属性时才显示相应控制。
- 控制状态同步：非标准命名的电源属性（如 `relay_state`）在读取到布尔值后也会更新设备开关状态，避免有值却不显示开关状态。
- 设备 API：`GET /api/v1/devices/{id}` 返回设备详情、可用的 `SIID/PIID/AIID` 和参数规格。设备行悬浮或键盘聚焦时显示“复制 API”，触屏设备始终显示。
- 远程控制：设备列表展示规格允许的开关、枚举属性和动作按钮；动作输入参数按 MIoT 规格提示用户，不猜测参数编号。
- API 响应校验：小米属性写入和动作响应必须带有 `code` 与 `result`，不再将缺少结果的响应误报为成功。
- 工程结构：入口迁移到 `cmd/gohome`，Web 与服务端编排在 `internal/app`；品牌协议模块在 `internal/providers/{xiaomi,ewelink,esp32}`；公共功能按 `internal/features` 分组。
- 一条 `make`：先运行测试与 `go vet`，再同时交叉编译 Linux amd64 和 Windows amd64。
- 设备管理 UI：MIoT 控件较多的摄像头、音箱、扫地机器人等设备会单独显示控制卡片；常用开关、模式、音量、清扫/回充等快捷项放在卡片顶部，其余设置和动作折叠到“更多设置与动作”，过滤重启、删除、格式化、解绑等危险或容易误触的动作。
- 用电统计：电量面板显示今日/本周统计槽位；已配置型号调用小米 `/v2/user/statistics`，读取 `stat_day_v3` 或旧版 `stat_day` 日记录，按本机时区汇总今日和本周（周一至当前）。
- 小米统计键按型号白名单配置（例如 `cuco.plug.cp2d`、`cuco.plug.cp4/cp4am/cp4m`、`chuangmi.plug.212a01`、`iot.plug.jdls1`、`zimi.plug.zncz01`、部分 `lumi` / `qmi` / `cuco` 插座）；未知型号不猜统计 key 和单位，数值显示 `—`。
- eWeLink：优先读取云端 `dailyKwh` / `monthlyKwh`，兼容支持型号返回的 `dayKwh` / `weekKwh`；云端缺少日/周读数时，采用本机功率采样估算并标注估算起点。
- 小米认证流程没有修改；eWeLink 现有普通账号登录与控制流程保留。

> 设备控制能力以具体设备型号、固件和小米 MIoT 规格中的 `write` 权限 / `actions` 为准。没有公开可写属性或动作的设备会保持只读，不会用错误的通用命令冒充控制成功。摄像头视频预览、厂商私有云台协议等不在通用 MIoT 控制的保证范围内。

## eWeLink 用电统计说明

- eWeLink 官方状态接口公开了 `dailyKwh` 和 `monthlyKwh`，单位为 `0.01 kWh`；`-99999` 表示该字段无数据。周累计不是所有型号都提供的通用云端字段，因此 GoHome 会优先使用设备返回的周统计，否则汇总本机保存的每日累计。
- 若型号既不返回周期电量，也不再向云端推送新的功率读数，本机只能显示首次采集起点及最近有效功率短时间外推的估算，不能准确补出升级前或遥测中断期间的真实总用电。
- 2026 年 SonoffLAN 上游说明部分云端功率/电流/电压更新已受限制；某些 UIID 的日/周/月/年能耗传感器需要设备支持的局域网协议。若你的插座长期只有一条旧功率值，请提供 eWeLink 设备型号和 UIID/硬件版本，后续才能判断是否应增加该型号的本地协议。

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

版本从当前 Git 仓库读取：`VERSION_PREFIX` 默认是 `0.2`，版本后缀为 `git rev-list --count HEAD`。每新增一个 Git commit，下一次 make 的版本号会递增；仅重复执行 make 不会递增提交数。最近一次提交的短哈希和 subject 会显示在页面底部版本号的悬浮提示中。

如果直接从不含 `.git` 的源码 ZIP 目录编译，Git 信息会回退为 `0.2.0 / source-archive-no-git-metadata`。建议将源码放在你自己的 Git 工作区中，再执行 make，这样版本号和提交提示才能读取你的真实历史。

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
GOHOME_ADDR="0.0.0.0:17880" ./dist/gohome-linux-amd64
```

Windows PowerShell：

```powershell
$env:GOHOME_ADDR = "0.0.0.0:17880"
.\dist\gohome-windows-amd64.exe
```

浏览器访问 `http://NAS_IP:17880`。首次会显示 GoHome 登录页，默认账号 `admin`、密码 `123456`。登录成功后浏览器会保存长期 Cookie；以后访问自动保持登录，不需要重复输入密码。

也可以使用 `scripts/start-linux.sh` 或 `scripts/start-windows.bat` 从项目目录启动。

## 数据与登录状态

首次运行自动创建 `data/`。主要文件包括：

```text
data/
├── gohome.json             # 设备、账号元数据、状态和日志
├── gohome.vault.json       # 加密后的品牌凭证
├── gohome.vault.key        # 本机 AES-GCM 加密密钥
└── gohome.session.key      # GoHome 登录 Cookie 的签名密钥（必须保留）
```

升级时请停止旧程序并完整备份、复制整个 `data/` 目录。必须保留 `gohome.vault.json`、`gohome.vault.key` 和 `gohome.session.key`，否则品牌凭证无法解密或旧浏览器 Cookie 无法继续使用。不要把 `data/` 提交到 Git 或公开分享。

初始管理员账号固定为 `admin`，初始密码固定为 `123456`，写在 `internal/app/auth.go` 中，不读取环境变量。登录 Cookie 为长期持久 Cookie，服务端使用 `data/gohome.session.key` 验签，重启 GoHome 不会自动退出。浏览器厂商可能限制 Cookie 的最长保存时间，因此不承诺浏览器侧严格永久。

小米 / eWeLink 云端令牌仍受服务端有效期和安全策略约束，不承诺永久有效；但通常升级程序和保留 `data/` 后不需要重新登录品牌账号。

**安全提示：** 默认密码极弱且是固定值。只在可信家庭 LAN 使用；请勿配置公网端口转发、反向代理公开访问或在不可信网络使用。

## 设备 API

API 使用 GoHome 登录 Cookie。命令行可以先登录并将 Cookie 保存到本地文件：

```bash
BASE='http://NAS_IP:17880'
JAR='gohome-cookie.txt'
curl -c "$JAR" -b "$JAR" -X POST "$BASE/api/auth/login" \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"123456"}'
```

查询设备列表：

```bash
curl -b "$JAR" "$BASE/api/v1/devices"
```

查询单台设备及控制能力：

```bash
curl -b "$JAR" "$BASE/api/v1/devices/DEVICE_ID"
```

注销 Cookie：

```bash
curl -b "$JAR" -c "$JAR" -X POST "$BASE/api/auth/logout"
```

通用开关命令（小米 / eWeLink 支持的开关设备）：

```bash
curl -X POST -b "$JAR" \
  'http://NAS_IP:17880/api/v1/devices/DEVICE_ID/command' \
  -H 'Content-Type: application/json' \
  -d '{"command":"turn_on"}'
```

可用命令：`turn_on`、`turn_off`、`toggle`。多路 eWeLink 设备还需传 `channel`，通道编号以设备 API 返回值为准。

写入小米 MIoT 可写属性：

```bash
curl -X POST -b "$JAR" \
  'http://NAS_IP:17880/api/v1/devices/DEVICE_ID/property' \
  -H 'Content-Type: application/json' \
  -d '{"siid":2,"piid":1,"value":true}'
```

执行小米 MIoT 动作：

```bash
curl -X POST -b "$JAR" \
  'http://NAS_IP:17880/api/v1/devices/DEVICE_ID/action' \
  -H 'Content-Type: application/json' \
  -d '{"siid":4,"aiid":1,"params":[]}'
```

上面的 `SIID/PIID/AIID` 仅为示例，必须使用 `GET /api/v1/devices/{id}` 中当前设备实际发现的能力值和输入参数。设备管理页的“复制 API”会按每台设备生成对应示例，不会包含云端 Token 或登录密码。

完整 API 描述：`GET /api/v1/openapi.json`。健康检查：`GET /api/health`。

## 今日 / 本周用电统计

- 小米：通过型号专用统计键请求 `/app/v2/user/statistics` 的日统计记录；把当天多通道记录求和得到今日 kWh，把本周（周一 00:00 到当前）日记录求和得到本周 kWh。型号统计 key 和单位换算有差异，当前只对已知型号表启用；未知型号不会把实时 W 值直接当作日用电。v0.2.15 起，缺失数值的项目会从界面隐藏。
- eWeLink：官方设备状态字段 `dailyKwh` / `monthlyKwh` 的单位为 0.01 kWh；部分型号会额外返回 `dayKwh` / `weekKwh`。官方普通状态接口没有保证所有型号都提供周统计，缺少周值时从已记录的每日累计汇总；周期数据缺失时不显示对应项目。
- eWeLink 云端没有提供日累计、但提供月累计时，GoHome 会在同一自然日内用连续月累计值的差计算增量；若日/月累计均缺失但能持续返回实时功率，则在后台状态轮询时用相邻两次功率的平均值积分。首次采样显示 `0.000 kWh`，含义是“从首次采集开始”，不是程序启动前当天已经消耗的总量；本周从周一以来记录到的每日数据求和。采样中断或跨自然日的差值不会强行分配到今天。
- 如果云端状态接口不再更新实时功率，或设备离线，估算值不会持续增长；这类设备需要支持的局域网功率协议才能获得更准确实时计量。eWeLink 2026 年起对部分型号的云端实时功率、电流、电压更新有限制。
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

---

## v0.2.18 实时设备状态更新

本版将整页数据轮询改为轻量 AJAX 状态更新：仅在已登录、可见的总览/设备管理页面启用。浏览器每 2 秒读取 `/api/devices/live?since=REVISION`；无变化返回 204，有变化只更新设备卡片和首页统计。后台只在活动页面租约存在时访问品牌云端，目标采集间隔 10 秒。

小米实时读取会批量请求当前 MIoT 状态，并修复 `relay_state`、`switch_state` 等开关属性误分类问题。eWeLink 实时刷新会优先使用当前状态接口返回值，避免使用设备清单的缓存参数。

详细升级步骤、轮询限制和源码基线见 `UPGRADE-v0.2.18.md` 与 `SOURCE_BASELINE.md`。


## v0.2.18 当前页 AJAX 状态同步

- 总览/设备管理页面可见且已登录时才续租状态同步；隐藏页面或切换离开后释放租约，无活动页面时不请求品牌云端。
- 后端品牌状态读取目标间隔 10 秒；前端每 2 秒请求 `/api/devices/live?since=REVISION`，无变化返回 204，只更新设备状态与首页计数，不整页刷新。
- 小米修复 MIoT 开关属性映射并批量读取状态；批量结果严格按设备 DID 分配。
- eWeLink 强制查询最新 REST 状态而不回退到设备清单旧缓存；目前不是 Cloud4 WebSocket 推送。
- 设备控制成功后只请求轻量 live delta；页面离开时的租约释放请求会排队，避免被进行中的心跳丢弃。
- 详见 `UPGRADE-v0.2.18.md`、`TEST-REPORT-v0.2.18.md` 和 `SOURCE_BASELINE.md`。
