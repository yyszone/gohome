# GoHome v0.2.20 升级说明

## 本次修复

针对“小米可以控制，但设备从米家 App 关闭后 GoHome 仍显示开启”的问题，修复同名 MIoT 属性误映射。

`chuangmi.plug.m3` 规格里有两个 `on`：

- `switch.on`：SIID 2 / PIID 1，真正插座继电器状态。
- `indicator-light.on`：SIID 3 / PIID 1，插座指示灯状态。

旧版把两个值都写进同一个 `Device.Power`，有时指示灯的 true 把继电器 false 覆盖。新版将指示灯状态独立存为 `indicator_light`，只有主开关属性更新设备电源状态。

还新增缺少真实 `switch.on` 读数时的日志诊断。不会将温度、LED 或其他 bool 属性作为电源状态。

## 登录和数据

- 不改小米账号登录、验证码、serviceToken、ssecurity 和 RC4 请求签名。
- 不改 eWeLink 登录、控制和实时同步。
- 保留原有 `data/`、`gohome.vault.json`、`gohome.vault.key`、`gohome.session.key`。
- 保留 `.gitignore`，不会把真实 `data/` 和密钥打进 ZIP。

## 升级建议

先备份，再解压到临时目录对比：

```bash
cd ~/gohome
cp -a data data.backup-$(date +%Y%m%d-%H%M%S)
mkdir -p /tmp/GoHome-v0.2.20
unzip -q ~/Downloads/GoHome-v0.2.20.zip -d /tmp/GoHome-v0.2.20
diff -ru --exclude=.git --exclude=data . /tmp/GoHome-v0.2.20/GoHome-v0.2.20 | less
```

确认要合并后覆盖源码，保留本地 `.git` 和 `data/`，然后：

```bash
go test ./...
go vet ./...
make
```

本 ZIP 不带二进制文件。Makefile 仍根据 Git 提交总数、短哈希、最近提交内容和构建日期生成版本元信息，并编译 Linux / Windows amd64。

## 测试范围与限制

本版包含真实规格结构的离线回归测试，模拟 `switch.on=false` 与 `indicator-light.on=true` 两种返回顺序。没有你的实际小米账号和插座，无法在此环境验证云端实机读数；升级后请在米家 App 关闭插座，等待 GoHome 状态同步周期，检查其是否显示关闭。若仍不正确，请提供 `StateIssue` 日志及设备型号（不要提供 serviceToken、Cookie、密码）。

源码基线限制见 `SOURCE_BASELINE.md`。
