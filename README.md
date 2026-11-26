# GoHome v0.2.31

## v0.2.31 小米设备状态日志分级

- 将 MIoT 单设备状态读取问题与小米账号/云端请求异常分开记录；日志直接显示受影响设备数量、名称和型号，并明确标注“账号连接正常，其他设备继续同步”。
- MIoT 规格库未收录型号（例如 `hmpace.bracelet.v3nfc`）会显示为设备能力/规格覆盖限制，不再写成含糊的“后台状态同步失败”，也不暗示账号登录异常。
- 真正的云端轮询异常保留独立日志分类；设备问题和账号请求问题采用不同的 5 分钟限频键，互不覆盖。
- 新增状态日志摘要测试，覆盖未收录型号、设备数量、日志截断和无错误设备。
- 源码 ZIP 默认构建版本为 `0.2.31`，静态资源缓存标识同步更新。

## v0.2.30 小米 MIoT 兼容性修正

- MIoT 规格索引保留同一型号的全部规格版本，默认使用最新版本；路由器属性坐标完全不匹配时，最多回退尝试两个旧版规格。
- 属性结果匹配兼容数字/字符串 `siid`、`piid` 和 `iid` 坐标；显式坐标失效时会尝试 `iid` 中的服务/属性编号。
- RB03/RA67 路由器属性归类为网络遥测及 Wi-Fi 设置，不再污染家电主电源状态；R3G/LV1 没有可读取属性时跳过轮询，不再报同步失败。
- C103 扫地机器人显示 `vacuum.status` 工作状态和电池，不再要求字符串 `switch.on` 返回布尔值；X08C 触屏音箱采用最新规格读取音量、静音和睡眠模式，notify-only 属性不参与轮询。
- 不改变当前小米 `serviceToken/ssecurity` 登录和凭证格式；官方新版 v2 API 使用 OAuth2 Bearer Token，未错误地用旧会话重试。

## v0.2.29 部署连接复用、自定义目录与简易管理员密码

- 远程部署增加 SSH ControlMaster 连接复用：先建立一条 SSH 主连接，后续架构检查、目录创建、SCP 上传、二进制替换和 systemd 重启共用该连接；使用 SSH 密码认证时一次部署只需输入一次密码。
- `DEPLOY_PATH` 现在作为明确的远程项目目录参数，默认 `/root/gohome`；允许指定如 `/opt/services/gohome` 的绝对路径，并在部署前自动创建目录。自定义目录必须与 `gohome.service` 的 `WorkingDirectory` 一致。
- 管理员密码最低长度从 12 位调整为 6 个字符，支持 `123456` 这类简单密码；仍校验当前密码、确认密码，并在修改后轮换 Session 密钥。建议仅在可信家庭局域网使用 6 位数字密码。
- 同步更新 README、升级说明、模块维护文档和回归测试。
- 源码 ZIP 默认构建版本为 `0.2.30`，前端静态资源缓存引用统一使用 `?v=0.2.30`。

## v0.2.28 设备识别、认证管理与一键部署

- 小米 MIoT 温湿度优先使用属性名称、服务元数据和单位识别，修复 `currentTemperature` 被误判成电流的问题。
- Roborock / Vacuum 类扫地机器人不会把 MIoT 数组状态识别为主电源开关；旧的错误电源状态在设备快照层过滤，清扫/返回充电座等动作保留。
- 摄像头设备增加 HTTP 视频源配置：MJPEG、MP4、WebM 直接预览；HLS 需浏览器原生支持。RTSP / 私有 P2P 需通过独立网关转换。
- 首次启动生成随机管理员密码，保存在 `data/gohome.admin.password`（0600）；登录 Cookie 长期有效并在访问时刷新，页面右上角可修改密码。
- 设备页、视频预览与操作栏进一步适配手机屏幕。
- 增加 `make deploy DEPLOY_HOST=root@IP DEPLOY_ARCH=arm64`，远程校验架构、原子替换服务实际运行的二进制，启动失败自动回滚。
- 新增 `docs/PATCHING.md`，将密码、摄像头、设备语义、MIoT 协议和部署拆分为独立模块，方便未来按模块生成 patch。

## v0.2.27 品牌视觉升级

- 新增 GoHome 专属智能家庭图标：以深海军蓝为底，融合青绿色智能家居轮廓、线路节点和暖黄色门廊；用于网页 favicon、登录页品牌标识、侧边栏 Logo、Apple Touch Icon 与 Web App Manifest。
- 页面视觉统一为深海军蓝导航栏 + 清爽浅色控制台，使用青绿作为主交互色；优化统计卡片、设备卡片、接入卡片、局域网发现横幅、运行日志和登录卡片的层次、留白、边框与阴影。
- 新增独立 `theme.css` 主题覆盖层，原 `app.css` 和页面交互脚本保留；移动端断点、设备卡片、品牌登录、开关控制等原有 DOM 与交互不变。
- 新增 16/32/48 像素 favicon、SVG favicon、ICO、96 像素侧栏 Logo、180 像素 Apple Touch Icon、192/512 像素 Web App 图标和 `site.webmanifest`。
- 静态资源 URL 带版本参数；首页使用 `no-cache`，CSS/JS/图标等静态文件缓存 24 小时，更新前端时不会长期卡在旧图标或旧样式。
- Makefile 在原 Git 仓库中继续按提交数生成版本；从不含 `.git` 的源码 ZIP 编译时使用 `SOURCE_VERSION=0.2.31`，不再错误显示为 `0.2.0`。
- 仅修改前端静态资源、静态缓存头与版本元数据，不修改设备数据库、品牌账号 ID、登录流程、Token、Vault、Session 密钥或 API。

## v0.2.26 Armbian ARM32/ARM64 编译与 systemd 守护

- Makefile 新增 Linux ARMv7 32 位和 ARM64 64 位交叉编译，默认 `make` 会一起生成 Linux amd64、ARMv7、ARM64 和 Windows amd64。
- 新增 `make armbian`，只编译魔百盒 / Armbian 常用的 ARMv7 与 ARM64 两种目标。
- 新增 systemd 服务模板和安装脚本：开机启动、异常退出自动重启、日志进入 journal，按 `uname -m` 自动选择对应二进制。
- 服务固定使用项目根目录作为工作目录，并将 `GOHOME_DATA_DIR` 指向项目根目录的 `data/`，二进制放在 `/root/gohome/` 时，数据会保存在 `/root/gohome/data/`。
- GoHome 收到 systemd 的 SIGTERM 或终端 Ctrl+C 时会停止状态轮询并优雅关闭 HTTP 服务。
- 服务不设置 CPU/内存硬限制，不额外启动常驻监控进程；无活动设备页面时，状态调度器仍按现有事件驱动逻辑休眠。

## v0.2.25 小米房间同步兼容 HA、补齐设备操作日志

- **小米房间数据按 HA 方式解析：** 优先解析 `gethome_merged` 的 `homelist`、`share_home_list`、`devices` 元数据，兼容家庭级设备以及 DID 为字符串或数字；只要仍有待补齐的 DID，就继续调用官方 Xiaomi Home 集成使用的 `gethome` 和 `get_dev_room_page`，按首页的 `home_id / room_id` 还原分页房间名称。
- **房间同步错误可见：** 房间元数据查询失败不会阻止设备清单同步，也不会使小米账号掉线；同步后会在运行日志中显示接口错误原因，或明确提示云端响应未能把 DID 关联到房间。
- **设备操作记入运行日志：** 小米主开关、eWeLink 单路/多路开关、MIoT 属性与动作在成功或失败时记录操作结果；eWeLink Token 刷新失败和控制失败也写入日志。
- **操作后刷新日志：** 控制成功后前端立即读取本地 `/api/logs`，不必手动刷新页面；进入“运行日志”仍会重新加载。
- **同步超时：** 小米登录后的设备/房间元数据补齐以及手动同步的请求上限放宽，给分页房间查询留出时间。
- 保留小米与 eWeLink 已有普通账号登录、凭证、Token、控制和能耗统计逻辑；未改变品牌账号 ID 或 Vault 数据格式。

## v0.2.24 页面按需状态同步、房间名称映射与品牌账号重命名

- **页面按需同步：** 只有已登录、浏览器可见且当前处于“总览”或“设备管理”页面时，才租用品牌状态同步。切换到“品牌接入 / 局域网发现 / 运行日志”、隐藏页面或退出登录后释放租约；多标签页独立租约，任一可见标签仍在使用时不会错误停止其他标签的状态同步。
- **空闲时不空转：** 后端状态调度器由每秒 ticker 改为事件唤醒。没有活动页面时阻塞等待，不再每秒唤醒 goroutine；页面打开、续租、隐藏/离开时才唤醒调度器。有活动页面时仍按约 10 秒目标间隔同步品牌云端，网页继续每 2 秒获取轻量本地状态增量。
- **eWeLink 房间名称：** 设备列表中的 `family.roomid` 是房间 ID，不是显示名称。普通登录/手动设备同步时额外读取一次 `/v2/family`，建立 `roomList[].id -> roomList[].name` 映射。10 秒实时状态轮询不请求该接口，已有房间名称保存在本地设备记录中。
- **小米房间名称：** 设备列表缺少 `room_name` 时，在普通登录/手动同步时补充读取 `v2/homeroom/gethome_merged`，用家庭房间 `dids` 映射到房间名称。该查询不加入实时状态轮询；接口不可用时不影响设备发现和登录。
- **保留已知房间：** 若这次房间接口没有返回名称，不用“未分区”覆盖数据库中已有的真实房间名；后续成功解析到新房间时才更新。
- **品牌账号重命名：** 已连接账号新增独立显示名称，支持重命名和清空恢复原始名称。通过 `PATCH /api/accounts/{id}` 更新 `displayName`；不修改账号 ID、Provider、原始 Label、区域、登录状态、Token、Cookie 或 Vault 凭证。
- 保留小米、电联/易微联既有登录、验证码、开关控制和电量统计流程；保留 `.gitignore` 与 `data/.gitkeep`。
- 不包含编译二进制、真实 `data/`、Vault/Session 密钥。

## 页面状态同步规则

| 当前页面状态 | 是否请求品牌云端状态 |
| --- | --- |
| 已登录 + 页面可见 + 总览 | 是 |
| 已登录 + 页面可见 + 设备管理 | 是 |
| 品牌接入 / 局域网发现 / 运行日志 | 否 |
| 浏览器标签隐藏 / 最小化到后台 | 否（释放该标签租约） |
| 退出登录 | 否 |
| 所有标签关闭或崩溃 | 租约到期后自动停止 |

## 品牌账号重命名 API

```http
PATCH /api/accounts/{id}
Content-Type: application/json

{"displayName":"楼上米家"}
```

将 `displayName` 设为空字符串即可恢复使用原始账号标签。这个字段只改变 GoHome 页面显示名称，不会触碰加密凭证库。

---


## v0.2.23 小米开关采用 eWeLink 式延迟状态确认

- 小米主开关点击后，云端 `prop/set` 成功即立即更新本地目标状态并返回，不再在 700ms 后主动执行一次 `prop/get`。小米云端可能短暂返回命令前的缓存状态，快速读回会造成刚关闭又跳回开启的错觉。
- 小米状态确认统一交给页面活跃期间的后台轮询，复用 eWeLink 的节奏：目标每 10 秒轮询一次。浏览器继续使用轻量 Live API 接收本地状态变化，不刷新整页。
- 增加命令时间戳保护：若某一轮小米状态请求在用户点击开关之前已经启动，即使它在命令成功之后才返回，也不能覆盖新命令状态；下一轮在命令之后启动的状态请求才可以确认或纠正真实开关状态。
- 请求失败/超时时同样应用时间戳保护：旧请求失败不能清除刚刚写入的目标状态；命令之后启动的新请求若仍无法读到状态，则按既有规则将状态置为未知。
- MIoT 主电源属性写入 API 也改为立即响应并走同一延迟确认逻辑，不再同步等待最长 5 秒的立即读回。
- 新增回归测试：旧轮询返回 ON 不得覆盖刚刚发出的 OFF；旧轮询失败也不得清除 OFF；命令之后启动的新轮询可以按真实状态纠正。
- 不改动小米登录、serviceToken、Vault、eWeLink 登录/控制协议、页面 AJAX 间隔和现有 Makefile；保留 `.gitignore`，不附带二进制。

### 状态显示规则

- 点击成功后按钮保持目标状态，不因 700ms 快速读回而回弹。
- 下一轮页面活跃状态轮询（通常不超过 10 秒，实际受云端响应时间影响）读取物理状态。
- 若下一轮确认了目标状态，页面保持不变；若云端明确返回相反状态，则以新读数为准；若读取失败，则按未知状态处理，不把旧值伪装成实时状态。

---

# GoHome v0.2.22

## v0.2.22 小米开关快速反馈与去除指示灯按钮

- 小米主继电器控制改为类似 eWeLink 的快速反馈：云端 `prop/set` 成功返回后立即把目标开关值写入本地设备状态并响应 HTTP，不再让用户等待同步读取。
- 在后台独立 Context 中延迟约 700ms 读取同一个主继电器属性，校正乐观状态；该读回不依赖浏览器页面或 HTTP 请求 Context，不会因为用户点击后切换页面而被取消。
- 同一设备连续控制时使用序号保护，较早的后台读回不能覆盖较新的控制命令。
- 前端在点击瞬间先更新开关样式和状态摘要；命令失败才恢复旧状态。收到服务端设备快照后仅更新对应设备，不刷新整个网页。
- `chuangmi.plug.m3` 的指示灯属性（SIID 3 / PIID 1）保留在设备状态区，但不再生成无用的“开启”MIoT 按钮；主继电器仍使用 SIID 2 / PIID 1。
- 修正易微联单通道设备的本地即时更新：设备只有一个通道时，同时更新通道状态和设备级 `power` 字段。
- 新增 M3 快速状态投影测试，确认更新主继电器不会改变指示灯和温度数据。
- 保留 `.gitignore`、登录凭证结构和原有 Makefile；本发布包不附带二进制。

## v0.2.21 修复小米 M3 双开关状态、按钮和 context canceled

- 修复 `chuangmi.plug.m3` 可能同时出现两条“开关状态”的问题。该型号真实继电器固定为 `switch.on`（SIID 2 / PIID 1），指示灯为 `indicator-light.on`（SIID 3 / PIID 1）；现在三个层次统一以 2/1 的读回值为准：`Device.Power`、设备摘要、属性行和主开关按钮。
- 无论旧数据库中原来存的是“开启”还是重复的“关闭/开启”，升级启动时都会把 M3 缓存的主电源状态置为未知，保留温度、电量、指示灯和控制能力；拿到新一轮 2/1 读取值后才重新显示开启/关闭。
- 每次 M3 状态刷新后再次规范化属性：只保留一条主“开关状态”；SIID 3 / PIID 1 独立显示为“指示灯”，永远不能覆盖插座状态。若读取失败，主状态和主开关 control value 置为未知，按钮不再保持旧的 `on`。
- 兼容 MIoT 对布尔属性返回 `true/false` 或 `1/0` 的情况，将读数、属性文案和控制值统一规范为布尔值与“开启/关闭”。
- 修复页面租约结束导致已在途的小米 `device_list` 请求被取消的问题：离开页面只停止发起下一轮同步，已开始的请求允许结束；单次请求取消/超时会标记开关状态未知，不会被误报成账号登录失效，也不会影响其他品牌继续轮询。
- 小米读取发生 `context canceled` 时，如果上层轮询仍然有效，会进行一次有界重试；如果本轮仍失败，会清除旧电源显示但保留 Vault 中昨天的登录数据。
- 前端对 M3 增加最后一道显示防线：旧数据中的重复“开关状态”不再渲染；主状态只从 `power` 字段生成，指示灯单独显示。
- 新增启动迁移、外部读到 OFF、numeric 0/1、属性去重、Vault 文件原样保留等回归测试。
- `.gitignore` 保留在源码 ZIP 根目录。

### 登录与数据

本版不修改小米用户名/密码登录、短信/邮箱验证、`serviceToken`、`ssecurity`、RC4 签名和 eWeLink 登录逻辑。**不需要重新登录小米账号**。升级迁移只修改 `gohome.json` 中 M3 的旧状态投影，不读取或覆盖 `gohome.vault.json`、`gohome.vault.key`、`gohome.session.key`。

## v0.2.20 修复小米插座状态总被指示灯覆盖

- 修复 `chuangmi.plug.m3` 等 MIoT 设备同名属性导致的状态覆盖：`switch.on` 表示插座继电器，`indicator-light.on` 表示设备指示灯。旧版把两个 `on` 都写入 `Device.Power`，如果 LED 结果最后返回且为 true，真实继电器即使为 OFF 也会被覆盖成 ON。
- 新版为 `indicator-light` / `LED` 服务建立独立 `indicator_light` 属性；只有实际电源/开关服务的 `on` 属性更新 `Device.Power`。无论云端先返回继电器还是指示灯，都不会相互覆盖。
- 保留指示灯状态为独立设备属性，页面可显示“指示灯 开启/关闭”，但它不再代表插座通断。
- 如果规格声明有可读开关属性、云端响应却没有有效 `switch.on` 值，日志会明确提示“未返回有效开关属性值”，不再因为温度等其他属性读成功就误判电源状态已读取。
- 小米登录、验证码、serviceToken、RC4 签名和属性写入流程没有修改；eWeLink 登录、控制和实时同步没有修改。
- `.gitignore` 保留在 ZIP 根目录。

### MIoT 规格依据

`chuangmi.plug.m3` 公开的 MIoT 规格包含 `Switch` 服务（SIID 2），其中 `on` 为 PIID 1；还包含 `Indicator Light` 服务（SIID 3），其 `on` 同样为 PIID 1。两个属性名称相同但含义不同，不能共用全局电源状态。参见 https://home.miot-spec.com/en/spec/chuangmi.plug.m3 。

## v0.2.19 小米 MIoT 实时状态读取修复

- 修复旧版 `xiaomiio` 云端状态读取的跨设备批量映射问题：部分 `/app/miotspec/prop/get` 结果不带 `did`，旧逻辑要求每条结果必须含 `did`，导致小米实时属性被全部丢弃。
- 小米实时属性改为**每台设备一个 `prop/get` 请求**，同一设备的多个 `siid/piid` 仍合并在一个请求中；最多 3 台并发，兼顾可靠性和云端请求压力。
- 仅在单设备隔离请求中允许响应省略 `did`；若响应明确带有其他设备的 `did`，仍然拒绝应用，防止串设备状态。
- 设备型号索引匹配增加大小写不敏感兜底；MIoT 规格、属性请求、业务 code 和未匹配结果会写入节流日志，便于判断是型号缺失、`access` 不含 `read`、云端错误还是响应字段不匹配。
- 新增 `.gitignore`，忽略运行数据、vault/session 密钥、日志、临时文件和编译目录，同时保留 `data/.gitkeep`。
- **没有改动小米登录 / serviceToken / 二次验证流程，也没有改动 eWeLink 登录与实时状态读取。**

## 小米读取协议说明

当前 GoHome 使用旧版 `sid=xiaomiio` 的 `serviceToken + ssecurity` 会话，因此本版继续使用兼容该会话的旧云端路径 `/app/miotspec/prop/get`，不是新版官方 Home Assistant 集成的 OAuth2 `/app/v2/miotspec/prop/get`。

新版官方 Xiaomi Home 集成使用 OAuth2，并通过 MIoT Cloud MQTT 订阅属性变化；旧 `xiaomiio` serviceToken 不能直接拿来当 OAuth2 Bearer Token 或 MQTT 密码。本版不更改登录流程，因此先修复旧接口的可靠读取，暂不声称支持官方 MQTT 实时推送。参见 `docs/XIAOMI-MIOT-STATE.md`。

## 构建

本源码包不包含二进制文件。保留原 Git 工作区后执行 `make`，按 Makefile 中 Git 提交总数生成版本元信息，并编译 Linux / Windows x64。请先阅读 `SOURCE_BASELINE.md`，确认本地没有未归档代码再合并。

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
- 首次访问显示 GoHome 内置登录页；管理员账号为 `admin`，首次随机密码保存在 `data/gohome.admin.password`，不使用共享默认密码。
- 登录后由服务端签发 HttpOnly、SameSite Strict 的长期 Cookie；签名密钥写入 `data/gohome.session.key`，进程重启后仍可验证旧 Cookie。已认证的 GET 请求会滚动刷新 Cookie。
- 页面支持修改管理员密码；修改成功后轮换 Session 密钥，其他浏览器的旧会话失效。新密码长度要求 6–128 个字符；支持 6 位数字，但短密码仅建议在可信家庭局域网使用。
- 登录失败 5 次后对来源 IP 暂时限流；API 复制示例先从服务端密码文件读取密码，再使用 Cookie 调用设备 API。

**安全警告：** 管理员密码和 Session 签名密钥保存在 `data/`，请保护整个目录（密码和 Session 文件为 0600），不要提交到 Git 或放入公开源码 ZIP。GoHome 可控制插座、摄像头等家庭设备，只能放在可信局域网内使用，不要把 17880 端口直接映射到公网。浏览器 Cookie 设为 10 年持久有效，但浏览器本身可能按自身策略限制最长保留时间；丢失 `data/gohome.session.key` 会使旧 Cookie 失效，需要重新登录。

GoHome 是一个轻量级 Go 家庭设备管理服务，单进程运行，静态 Web 资源内嵌在可执行文件中。当前支持小米 / 米家云端 MIoT 与 eWeLink 普通账号，提供设备同步、状态读取、规格驱动控制、设备 API、日志和 mDNS 发现扩展。

## v0.2.15 历史变更（已由 v0.2.17 页面租约策略接管轮询时机）

- 后端新增品牌云端状态轮询：小米使用设备列表和在线设备 MIoT 属性读取，eWeLink 使用账号设备清单及可用状态接口读取；实际更新时延取决于设备数量、云端响应速度和网络状况。v0.2.17 已进一步限制为只有总览/设备管理页面活跃时才执行。
- 只更新已导入设备，不自动添加新设备、不恢复用户隐藏设备；云端请求失败时保留最后一次有效状态，并在日志中按账号节流记录。
- 电量面板只渲染真实存在的数值；缺少今日/本周值时隐藏对应指标，不再显示 `— kWh`。
- 登录页清除账号预填和默认账号/密码提示，认证逻辑与长期 Cookie 不变。


本项目使用 Go + Makefile 编译，不使用 Docker、Node.js，也不需要前端构建。本 v0.2.17 源码包不附带编译产物；已执行 `go test ./...`、`go test -race ./...`、`go vet ./...` 和前端 JavaScript 语法检查，双平台可执行文件由你在原 Git 工作区运行 `make` 生成。完整源码来源限制见 `SOURCE_BASELINE.md`。

## v0.2.14 更新

- 内置管理员登录页：`POST /api/auth/login` 校验首次启动生成的管理员密码；首次进入先登录，API 未认证时返回 JSON 401。
- 长期登录 Cookie：`gohome_session` 为 HttpOnly、SameSite Strict，过期时间设置为 10 年；密钥 `data/gohome.session.key` 使用随机 256-bit 值创建并以 0600 权限保存。管理员密码另存为 `data/gohome.admin.password`，升级时必须保留整个 `data/` 目录。
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
├── scripts/                     # 启动、systemd 安装脚本
├── deploy/systemd/               # systemd 服务模板
├── Makefile                     # Linux amd64/ARMv7/ARM64 + Windows amd64
├── go.mod
├── README.md
└── THIRD_PARTY_NOTICES.md
```

说明：小米登录、验证码和 STS 会话编排仍位于 `internal/app` 的 `providers_xiaomi_auth.go`。这是为了保持当前已经验证可用的登录流程不变；小米 MIoT 控制能力已经独立在 `internal/providers/xiaomi`。eWeLink 与 ESP32 也有独立协议目录。

## 环境要求

- Go 1.23 或更高版本
- GNU Make（Windows 可通过 MSYS2 / Git Bash 等提供 make；也可以直接运行 `go build`）
- 推荐在开发机交叉编译目标架构；发布目标均使用 `CGO_ENABLED=0`，无需在性能较弱的 Armbian 盒子上安装 Go

## 编译

### 默认：编译常用发布平台

```bash
make
```

默认目标会依次执行：

1. `go test ./...`
2. `go vet ./...`
3. 编译 Linux amd64
4. 编译 Linux ARMv7 32 位
5. 编译 Linux ARM64 64 位
6. 编译 Windows amd64

版本从当前 Git 仓库读取：`VERSION_PREFIX` 默认是 `0.2`，版本后缀为 `git rev-list --count HEAD`。每新增一个 Git commit，下一次 make 的版本号会递增；仅重复执行 make 不会递增提交数。最近一次提交的短哈希和 subject 会显示在页面底部版本号的悬浮提示中。

如果直接从不含 `.git` 的源码 ZIP 目录编译，Git 信息会回退为 `0.2.0 / source-archive-no-git-metadata`。建议将源码放在你自己的 Git 工作区中，再执行 make，这样版本号和提交提示才能读取你的真实历史。

输出：

```text
dist/gohome-linux-amd64
dist/gohome-linux-armv7
dist/gohome-linux-arm64
dist/gohome-windows-amd64.exe
```

### 其他编译命令

```bash
make build       # 编译当前平台到 build/
make run         # 编译并运行当前平台
make dev         # go run ./cmd/gohome
make test        # 单元测试
make vet         # 静态检查
make release     # 与默认 make 相同，测试后编译全部发布目标
make linux       # 只编译 Linux amd64
make linux-arm32 # 只编译 Linux ARMv7 32 位（GOARM=7）
make linux-arm64 # 只编译 Linux ARM64 64 位
make armbian     # 执行测试和静态检查，只编译 Armbian ARMv7 + ARM64
make windows     # 只编译 Windows amd64
make clean       # 删除 build/ 和 dist/
```

注意：`gohome-linux-armv7` 适用于 32 位 ARMv7 用户空间，`gohome-linux-arm64` 适用于 64 位 AArch64 用户空间。不要仅凭 CPU 型号判断系统位数，部署前在盒子上执行 `uname -m`；`aarch64` 选 ARM64，`armv7l` 选 ARMv7。极老的 ARMv6 系统不属于默认 ARMv7 目标。

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

浏览器访问 `http://NAS_IP:17880`。首次会显示 GoHome 登录页，管理员账号为 `admin`。在服务端运行 `cat data/gohome.admin.password` 读取首次随机生成的密码。登录成功后浏览器会保存长期 Cookie；以后访问自动保持登录。

也可以使用 `scripts/start-linux.sh` 或 `scripts/start-windows.bat` 从项目目录启动。手动运行 Linux 二进制时请先 `cd` 到项目根目录，或设置绝对路径 `GOHOME_DATA_DIR`；Armbian 的 systemd 服务已自动固定为 `/root/gohome/data`。

## 魔百盒 / Armbian 部署（ARM32 / ARM64）

建议在电脑或已有 NAS 上交叉编译，再把二进制复制到魔百盒；不需要在性能较弱的盒子上安装 Go，也不需要 Docker。

### 1. 确认 Armbian 架构

在魔百盒执行：

```bash
uname -m
getconf LONG_BIT
```

- `aarch64` 且 `64`：使用 `gohome-linux-arm64`。
- `aarch64` 但 `32`（64 位内核 + 32 位用户空间）：使用 `gohome-linux-armv7`。
- `armv7l` 且 `32`：使用 `gohome-linux-armv7`。
- `armv6l`：默认 ARMv7 包不适用，需要单独按 ARMv6 编译。

### 2. 交叉编译

在有 Go 1.23+ 和 GNU Make 的开发机进入项目目录：

```bash
make armbian
```

产物在 `dist/`：

```text
dist/gohome-linux-armv7
dist/gohome-linux-arm64
```

### 3. 放到魔百盒 `/root/gohome`

把源码 ZIP 直接解压到魔百盒 `/root/gohome`，再把对应架构的二进制也放到这个目录。假设 ZIP 已上传到 `/root/GoHome-v0.2.26.zip`：

```bash
mkdir -p /root/gohome
unzip -oq /root/GoHome-v0.2.26.zip -d /root/gohome
# 如果使用单独提供的 ARM 预编译包，也解压到相同目录：
unzip -oq /root/GoHome-v0.2.26-Armbian-binaries.zip -d /root/gohome
```

解压后，`Makefile`、`scripts/`、`deploy/` 以及对应的 `gohome-linux-armv7` / `gohome-linux-arm64` 二进制应直接位于 `/root/gohome`，不要多出一层 `GoHome-v0.2.26/` 子目录。目录大致如下：

```text
/root/gohome/
├── gohome-linux-armv7       # 32 位系统使用
├── gohome-linux-arm64       # 64 位系统使用
├── scripts/
│   ├── service-start.sh
│   └── install-systemd.sh
├── deploy/systemd/gohome.service
└── data/                    # 首次启动自动创建
```

如果二进制来自开发机的 `dist/`，例如在开发机执行：

```bash
scp dist/gohome-linux-arm64 root@ARMBIAN_IP:/root/gohome/
scp dist/gohome-linux-armv7 root@ARMBIAN_IP:/root/gohome/
```

只需复制与你的 Armbian 用户空间匹配的那个二进制；也可以两个都复制，服务启动脚本会结合 `uname -m` 和 `getconf LONG_BIT` 自动选择，兼容 64 位内核运行 32 位用户空间。源码目录中的 `scripts/` 和 `deploy/` 也必须保留。

### 4. 安装开机启动和守护服务

在魔百盒执行：

```bash
cd /root/gohome
chmod +x scripts/service-start.sh scripts/install-systemd.sh
bash scripts/install-systemd.sh
```

安装脚本会生成 `/etc/systemd/system/gohome.service`，并启用和启动服务。服务行为：

- 开机后在网络就绪时自动启动；
- GoHome 异常退出后 3 秒自动重启；
- 收到 SIGTERM 时先停止状态轮询，再给 HTTP 请求最多 10 秒优雅退出；
- 日志交给 systemd journal，不在项目目录不断生成大日志文件；
- 工作目录固定为 `/root/gohome`，数据目录固定为 `/root/gohome/data`；
- 不设置 CPU/内存硬限制，不额外运行常驻监控进程。启动脚本 `exec` 真正的 Go 二进制，运行后不会保留 shell 父进程。

### 5. 查看状态与日志

```bash
systemctl status gohome --no-pager
journalctl -u gohome -n 100 --no-pager
journalctl -u gohome -f
```

常用管理命令：

```bash
systemctl restart gohome       # 重启
systemctl stop gohome          # 停止
systemctl start gohome         # 启动
systemctl disable --now gohome # 取消开机启动并停止
```

浏览器访问 `http://魔百盒IP:17880`。默认监听 `0.0.0.0:17880`，请只在可信家庭局域网开放，不要将端口直接映射到公网。

### 6. 数据目录与更新

即使程序文件在 `/root/gohome`，所有运行数据也集中在：

```text
/root/gohome/data/
├── gohome.json
├── gohome.vault.json
├── gohome.vault.key
└── gohome.session.key
```

systemd 服务显式设置 `GOHOME_DATA_DIR=/root/gohome/data`，不依赖从哪个终端目录启动。更新二进制时先备份数据，再停止服务、替换二进制并启动：

```bash
cd /root/gohome
cp -a data "data.backup-$(date +%Y%m%d-%H%M%S)"
systemctl stop gohome
# 将新版本对应架构的二进制复制到这里，并保留文件名
chmod 0755 gohome-linux-arm64  # 64 位系统；32 位则改为 gohome-linux-armv7
systemctl start gohome
journalctl -u gohome -n 50 --no-pager
```

**不要删除或覆盖 `data/`。** 更换二进制不需要重新登录小米或 eWeLink；Vault 和 Session 密钥必须与原数据一起保留。

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

管理员账号固定为 `admin`，首次密码由每个安装独立随机生成并写入 `data/gohome.admin.password`。登录 Cookie 使用 `data/gohome.session.key` 验签，Cookie 有效期为 10 年并在认证访问时刷新；修改管理员密码会轮换密钥，使其他浏览器旧会话失效。浏览器厂商可能限制 Cookie 的最长保存时间。

小米 / eWeLink 云端令牌仍受服务端有效期和安全策略约束，不承诺永久有效；但通常升级程序和保留 `data/` 后不需要重新登录品牌账号。

**安全提示：** 首次密码为每个安装独立随机生成，但仍建议首次登录后立即修改为自己保管的强密码。只在可信家庭 LAN 使用；请勿配置公网端口转发、反向代理公开访问或在不可信网络使用。

## 设备 API

API 使用 GoHome 登录 Cookie。命令行可以先登录并将 Cookie 保存到本地文件：

```bash
BASE='http://NAS_IP:17880'
JAR='gohome-cookie.txt'
# 在 NAS 上读取首次生成的随机密码；也可以手动输入你修改后的密码。
PASSWORD="$(ssh root@NAS_IP 'cat /root/gohome/data/gohome.admin.password')"
curl -c "$JAR" -b "$JAR" -X POST "$BASE/api/auth/login" \
  -H 'Content-Type: application/json' \
  -d "{\"username\":\"admin\",\"password\":\"$PASSWORD\"}"
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

## 小米开关快速反馈

MIoT 主继电器控制在云端 `prop/set` 成功后立即更新本地状态并返回，不再把第二次云端读取放在 HTTP 响应路径中。服务端随后使用独立后台 Context 延迟执行一次精确属性读回，校准真实状态。浏览器端在点击时立即切换按钮，成功后合并服务端设备快照，失败时回滚；无需刷新整页。`chuangmi.plug.m3` 的指示灯（3/1）仍显示为状态，但不会再生成额外的属性开关按钮。

## v0.2.29 一键部署

默认部署到远程 `/root/gohome`，已安装并运行 `gohome` systemd 服务时，在开发机执行：

```bash
make deploy DEPLOY_HOST=root@192.168.31.63 DEPLOY_ARCH=arm64
```

部署到其他目录时，显式设置 `DEPLOY_PATH`：

```bash
make deploy DEPLOY_HOST=root@192.168.31.63 DEPLOY_ARCH=arm64 DEPLOY_PATH=/opt/services/gohome
```

`DEPLOY_PATH` 必须是 GoHome 项目根目录的绝对路径，默认 `/root/gohome`。自定义目录需先将完整项目目录放到该位置，并在该目录执行 `sudo sh scripts/install-systemd.sh`，让 systemd 的 `WorkingDirectory` 与部署目录保持一致；不能只改部署参数而让 systemd 仍指向旧目录。`DEPLOY_SERVICE` 可覆盖服务名。

部署脚本使用 SSH ControlMaster 复用连接：SSH 密码认证时，建立主连接时输入一次密码，之后的架构检查、目录创建、SCP 上传和服务重启复用该连接。命令自动测试、交叉编译、检查远程架构、上传、原子替换并重启服务；如果新进程未进入 active 状态，会恢复旧二进制。只更新 `scripts/service-start.sh` 实际选择的架构二进制，不清理 `data/`。详见 `UPGRADE-v0.2.29.md`。

后续按模块生成和应用 Patch 的目录分工见 `docs/PATCHING.md`。
