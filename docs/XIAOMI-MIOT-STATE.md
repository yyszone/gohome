# 小米 MIoT 状态读取：认证、接口和限制

## 1. 设备规格描述“哪些属性可读”

MIoT Spec 的属性会列出 `siid`、`piid`、`format`、`unit` 和 `access`。读取路径只选择声明支持 `read` 的属性；`write` 决定能否写入，`notify` 表示设备属性变化通知能力。不能把 `notify` 当作一定能通过旧云端 `prop/get` 读取，也不能把“可写”当作“当前值已成功读取”。

小米 MIoT Spec 官方协议入口：
- https://miot-spec.org/miot-spec-v2/instances?status=all
- https://miot-spec.org/miot-spec-v2/instance?type={type}
- MIoT Spec overview: https://miot-spec.org/

## 2. 当前 GoHome 使用的旧账号会话

GoHome 当前成功的登录方式属于旧版 `xiaomiio` 账号会话，保存 `userId`、`serviceToken` 和 `ssecurity`。因此当前实时查询继续走旧接口：

```text
POST https://{region}.api.io.mi.com/app/miotspec/prop/get
form: data / rc4_hash__ / signature / ssecurity / _nonce
Cookie: userId / serviceToken / timezone / locale 等
payload: {"params":[{"did":"...","siid":2,"piid":1}, ...]}
```

请求可以把同一台设备的多个属性放进 `params`。为了处理旧云端响应省略 `did` 的情况，GoHome v0.2.19 不再跨设备混合 DID：每个请求只包含一个设备的属性。此时响应没有 `did` 可安全按该设备的 `siid/piid` 映射；如果响应显式带了其他 DID，仍会拒绝该记录。

旧版社区集成 `hass-xiaomi-miot` 的 `async_get_properties_for_mapping(did, mapping)` 同样按单个 did 构造属性参数，再将响应按 `siid/piid` 映射：
- https://github.com/al-one/hass-xiaomi-miot/blob/master/custom_components/xiaomi_miot/core/xiaomi_cloud.py
- https://github.com/al-one/hass-xiaomi-miot/blob/master/custom_components/xiaomi_miot/core/device.py

## 3. 新版官方 Xiaomi Home 的 OAuth2 / MQTT 路径不是同一套认证

小米官方 `ha_xiaomi_home` 使用 OAuth2 access token；其 HTTP 客户端使用 Bearer access token 以及新版 `/app/v2/miotspec/prop/get` 路径，并包含 `datasource: 1`。该集成还订阅 MIoT Cloud MQTT 属性变化消息，因此不需要靠持续轮询来获知每次变化。

官方资料：
- https://github.com/XiaoMi/ha_xiaomi_home
- https://github.com/XiaoMi/ha_xiaomi_home/blob/main/README.md
- https://github.com/XiaoMi/ha_xiaomi_home/blob/main/custom_components/xiaomi_home/miot/miot_cloud.py

**不能把 GoHome 现有 `serviceToken/ssecurity` 直接当作 OAuth2 Bearer Token 或 MQTT 密码。** 若要获得官方集成相同的云端 MQTT 推送，需要单独增加 OAuth2 授权流程、refresh token 存储、region-aware MQTT TLS 连接、订阅/重连和设备消息解析。这会增加一套登录方式，不能在不改变现有认证的情况下“直接开启 MQTT”。

官方 HA 集成仓库含有小米限定许可声明，其代码授权仅限 Home Assistant 非商业用途，不应直接复制其代码到 GoHome；本项目只按公开协议行为独立实现兼容读取，不复制其源代码。

## 4. v0.2.19 读取失败如何定位

进入 GoHome 运行日志查看。v0.2.31 起，单个设备的 MIoT 能力/规格问题与云端账号请求问题分开记录：设备问题使用“有 N 台设备实时状态未同步（账号连接正常，其他设备继续同步）”；账号/云端请求问题使用“云端状态请求异常”。

- `MIoT 规格库未收录型号`：MIoT model/type 索引不含该型号或 `model` 字段异常。
- `读取 MIoT 设备规格失败`：下载规格实例失败/JSON 不兼容。
- `MIoT 规格中没有可读取的实时属性`：该型号规格中没有合适的 `access: read` 属性。
- `请求 MIoT prop/get 失败`：HTTP、网络、签名或服务端响应错误。
- `MIoT prop/get 业务错误`：云端 JSON envelope 返回非零业务 code。
- `没有匹配到请求的 SIID/PIID`：云端返回了记录，但与规格定义的属性 ID 不一致。

日志只包含设备名/model 和经过脱敏的错误摘要，不包含 Cookie、serviceToken、ssecurity、密码或验证码。

## 5. 同名 `on` 属性必须按服务区分（v0.2.20）

部分设备在不同 MIoT 服务中都定义了 `on`，不能只凭属性名称判断它是不是设备主电源。典型例子是 `chuangmi.plug.m3`：

- `switch` 服务：SIID 2，属性 `on` 是 PIID 1，表示插座继电器开/关。
- `indicator-light` 服务：SIID 3，属性 `on` 也是 PIID 1，只表示指示灯开/关。

旧版解析器把两个属性都映射到 `Device.Power`。当云端返回顺序为 `switch.on=false`、`indicator-light.on=true` 时，指示灯结果会覆盖插座真实状态，因此页面看起来总是“开启”，但写入控制仍然正常。

v0.2.20 按 service name / service type 分类：指示灯属性存为独立的 `indicator_light`，只有设备开关服务的 `on` 属性能更新 `Device.Power`。新增回归测试分别验证两种云端返回顺序，确保结果都为插座关闭、指示灯开启。

如果 MIoT 规格声明存在主开关属性，但实时响应没有返回该属性或返回该属性的非零状态码，GoHome 会记录“未返回有效的开关属性值（switch.on）”，不会把 LED、温度或其他属性当作开关状态。


## 6. v0.2.21：M3 主继电器、指示灯和旧状态迁移

`chuangmi.plug.m3` 的公开规格将主继电器与指示灯分在不同服务：

- `switch.on`：SIID 2 / PIID 1，代表插座实际通断。
- `indicator-light.on`：SIID 3 / PIID 1，代表设备指示灯。

GoHome 的设备 `power`、主开关按钮、设备状态摘要和“开关状态”属性现在全部由主继电器 2/1 决定。3/1 只作为“指示灯”属性。读数 `false` 或数值 `0` 都必须显示“关闭”；`true` 或数值 `1` 显示“开启”。

升级时会将旧版 M3 的缓存 `power` 和主电源属性清空，防止旧数据继续显示“已开启”；会保留米家账户、Vault/Session 文件、温度和电量遥测。下一次云端状态读取成功后重新建立唯一的主开关状态。如果读回失败，前端显示“开关状态未知”，而不是用指示灯状态或旧值代替。

## 7. `context canceled` 的含义

`context canceled` 是 Go 的请求上下文被取消，不等于 `serviceToken` 失效。常见原因是旧版页面活动租约释放时取消了正在进行的设备列表请求，或某一轮采集超时。v0.2.21 调整为：页面离开只阻止启动下一轮，不取消已启动请求；孤立取消在上层仍有效时有界重试一次。若仍失败，会清除旧的主电源状态并记录“本轮请求被取消/超时，不是登录失效”。这不会删除或重写小米 Vault 凭证。


## 8. v0.2.30：路由器、扫地机器人和触屏音箱的兼容读取

- **规格版本选择：** MIoT instances 索引可能为同一 model 返回多个 type 版本。GoHome 按 URN 尾部数字版本降序排列，正常情况优先使用最新规格。
- **老路由器固件回退：** `xiaomi.router.rb03` / `xiaomi.router.ra67` 若最新规格请求返回记录但坐标完全不匹配，会最多尝试两个较旧的公开规格版本；只在旧规格确实匹配到响应记录后才应用，不会拿任意结果填充错误字段。
- **属性标识兼容：** 结果优先尝试 `siid` + `piid`（支持数字或数字字符串）；如果结果同时提供 `iid`（例如 `0.2.1`），也会解析末两段作为备用坐标。日志只输出请求/响应坐标摘要，不输出属性值和凭证。
- **路由器状态：** 下载/上传速率、在线时长、连接数、状态、信号、Wi-Fi/访客 Wi-Fi 和指示灯均为网络遥测或独立设置，不写入家电的 `Device.Power`。
- **不支持读取的旧型号：** 若规格所有属性都不含 `access: read`，GoHome 跳过 `prop/get`，保留设备在线状态，不再记作云端实时同步失败。此类设备若要读取更多信息，需要另做路由器 LAN API 驱动，并非修改 MIoT 轮询可以得到。
- **扫地机器人 C103：** `vacuum.status` 是工作状态（待机、清扫、回充等）；`vacuum.on` 不是通用电源布尔值，GoHome 不把它映射为开关。
- **触屏音箱 X08C：** 采用索引中最新 MIoT 规格，读取可读的音量、静音和睡眠模式属性；只有 `notify`、没有 `read` 的播放状态不会进入 `prop/get`。

### 官方新版 v2 接口的认证区别

小米官方 `ha_xiaomi_home` 使用 `/app/v2/miotspec/prop/get` + `datasource: 1`，请求头使用 `Authorization: Bearer <OAuth2 access_token>` 与 AppId。GoHome 当前登录仍是旧 `serviceToken/ssecurity` 体系，不能仅把旧会话请求路径改成 `/v2/`。因此 v0.2.30 不会用旧会话盲目重试 OAuth2 接口；若以后需要切换官方 v2 / MQTT，必须独立实现 OAuth2 授权码、refresh token 和区域服务端。

若 RB03 / RA67 在新版本仍报告无法匹配，需从日志附加信息取得请求 SIID/PIID 和响应坐标（日志不会含属性值），以确认该固件返回的 `iid` 结构；不能通过直接放宽匹配条件把未知属性误映射到路由器状态。


## 9. v0.2.31：设备级问题不再伪装成账号同步失败

MIoT 规格库没有收录某个设备型号（例如 `hmpace.bracelet.v3nfc`）只表示 GoHome 暂时没有该设备的可用 MIoT 属性规格，不等于小米账号掉线、Token 失效或设备离线。v0.2.31 把这类 `Device.StateIssue` 与账号级云端请求错误分开记录：

```text
WARN [小米 / 米家] 有 1 台设备实时状态未同步（账号连接正常，其他设备继续同步）：
小米手环3 NFC版（hmpace.bracelet.v3nfc）：型号暂未收录到 MIoT 规格库，GoHome 暂时无法读取该设备的实时状态（不是设备离线，也不是账号登录失败）
```

设备级问题继续使用 WARN 以便被发现，但只影响明确列出的设备；其他设备的状态读取和账号连接维持正常。设备级和云端请求级日志分别限频，互不覆盖。
