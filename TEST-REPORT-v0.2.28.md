# GoHome v0.2.28 测试记录

测试日期：2026-11-26

本记录对应 `GoHome-v0.2.28-source.zip` 中的源码。测试覆盖本地 Go 代码、前端 JavaScript 语法、部署脚本及 ARM 交叉编译；没有连接用户的真实小米/eWeLink 账号，也没有在家庭局域网中验证真实摄像头视频源。

## 自动化检查结果

| 检查 | 结果 | 说明 |
| --- | --- | --- |
| `go test ./...` | 通过 | 全部 Go 单元测试通过 |
| `go test -race ./...` | 通过 | 全部 Go 测试通过竞态检测 |
| `go vet ./...` | 通过 | 未发现静态分析问题 |
| `node --check internal/app/web/app.js` | 通过 | JavaScript 语法检查 |
| `sh -n scripts/deploy.sh scripts/service-start.sh scripts/install-systemd.sh scripts/start-linux.sh` | 通过 | Shell 语法检查 |
| `make -n deploy DEPLOY_HOST=root@192.168.31.63 DEPLOY_ARCH=arm64` | 通过 | Make 部署目标展开正确；未连接真实 NAS |
| `make armbian` | 通过 | 自动测试/静态检查后，成功构建 ARMv7 32 位和 ARM64 64 位 ELF |

交叉编译产物已检查为静态链接 Linux ELF：`gohome-linux-armv7` 为 ARM EABI5 32 位，`gohome-linux-arm64` 为 AArch64 64 位。源码 ZIP 不包含这些构建产物，用户可在本机重新构建。

## 新增回归测试

- 管理员密码首次随机生成、重复启动复用、文件权限 `0600`。
- 修改管理员密码、Session 密钥轮换、旧 Cookie 失效、新 Cookie 保持登录、重启后新密码继续有效。
- 摄像头地址 HTTP/HTTPS 校验，拒绝 RTSP、`javascript:` 和 URL 用户名/密码；设备 API 可保存和清除地址。
- MIoT 服务元数据和单位识别温度/湿度；避免 `currentTemperature` 被误归类为电流。
- Roborock 数组状态不会生成主电源开关；历史缓存中的伪电源状态在设备快照中清理。
- 保留扫地机器人非电源布尔属性状态和清扫/回充类 MIoT 动作能力。

## 尚未完成的真实设备联调

- 尚未使用用户家中的温湿度计型号和云端规格确认其所有属性命名；不同型号仍可能需要针对性映射。
- 尚未对具体扫地机器人型号的完整 MIoT 动作列表进行现场验证。
- 尚未连接真实摄像头验证视频地址。网页只支持浏览器可访问的 HTTP/HTTPS 流；RTSP、小米私有 P2P 流需要 go2rtc、MediaMTX 等网关转换后才能播放。
- 部署目标只做了本地 dry-run 和脚本语法检查，未向用户的 NAS 执行真实上传或 systemd 重启。
