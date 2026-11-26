# GoHome v0.2.31 升级说明

## 本次修改

本版主要修正小米 MIoT 运行日志的表达范围，避免某一台设备的规格库缺失被误读为整个小米账号同步失败。

### 设备级问题

当某台设备的 `StateIssue` 非空，例如：

```text
小米手环3 NFC版 (hmpace.bracelet.v3nfc)
MIoT 规格库未收录型号 hmpace.bracelet.v3nfc
```

运行日志改为：

```text
WARN [小米 / 米家] 有 1 台设备实时状态未同步（账号连接正常，其他设备继续同步）：小米手环3 NFC版（hmpace.bracelet.v3nfc）：型号暂未收录到 MIoT 规格库，GoHome 暂时无法读取该设备的实时状态（不是设备离线，也不是账号登录失败）
```

设备仍保留在设备列表中。该提示只说明 GoHome 的 MIoT 规格库暂时无法为此型号构建实时属性读取计划，不代表账号登录失败或设备离线。

### 云端/轮询请求问题

真正的云端列表、HTTP 请求、会话或状态轮询错误独立显示为：

```text
WARN [小米 / 米家] 云端状态请求异常：...
```

该日志仍需要结合错误内容判断是网络、接口、请求签名还是会话问题，不能仅凭 WARN 认定密码错误。

### 限频行为

- 设备级状态问题使用 `device-state-poll|账号ID` 限频键。
- 云端/轮询流程问题使用 `state-poll|账号ID` 限频键。
- 两种日志分别 5 分钟限频，互不抑制。
- 一条日志最多展开 5 台设备；其余数量会明确显示。

## 升级方式

建议解压到新的开发目录，先测试再部署：

```bash
unzip GoHome-v0.2.31-source.zip -d ~/gohome-v0.2.31
cd ~/gohome-v0.2.31/GoHome-v0.2.31

go test ./...
go vet ./...

make deploy DEPLOY_HOST=root@192.168.31.63 DEPLOY_ARCH=arm64
```

部署时远程项目目录默认 `/root/gohome`，可通过 `DEPLOY_PATH=/opt/services/gohome` 指定其他目录；目标路径必须与 systemd 服务的 `WorkingDirectory` 一致。

本次不修改小米认证流程、Vault 凭证、Session 密钥、设备数据库或设备控制协议。
