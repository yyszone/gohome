# GoHome v0.2.21 升级说明

## 本版修复什么

你给出的“小冰箱”页面曾同时显示两条“开关状态：关闭/开启”，底部摘要和开关按钮又始终显示开启。根因是 `chuangmi.plug.m3` 有两个名字都叫 `on` 的属性：

- `switch.on`：SIID 2 / PIID 1，真正插座继电器。
- `indicator-light.on`：SIID 3 / PIID 1，指示灯。

v0.2.21 统一让设备 `power`、主开关按钮、摘要和唯一一条“开关状态”都使用 2/1。3/1 单独显示“指示灯”。bool 与数值 0/1 都会规范化。

旧的 M3 缓存状态会在启动迁移时清为“未知”，首次状态读取成功后恢复。这样不会继续拿昨天的“开启”当作实时状态。

## 是否需要重新登录

**不需要重新登录小米，也不要删除账号。** 本次没有修改小米登录和令牌流程。迁移只更新 `data/gohome.json` 中小冰箱的旧状态投影，不会覆盖：

```text
data/gohome.vault.json
data/gohome.vault.key
data/gohome.session.key
```

这些文件必须原样保留。

`context canceled` 表示请求上下文被取消/超时，不代表 serviceToken 过期。v0.2.21 不会因页面租约结束而取消已经发出的请求；仍失败时把电源状态置未知并保留登录凭证。

## 推荐升级步骤

先备份运行数据：

```bash
cd ~/gohome
cp -a data data.backup-$(date +%Y%m%d-%H%M%S)
```

解压到临时目录比较，**先不要直接覆盖生产目录**：

```bash
mkdir -p /tmp/GoHome-v0.2.21
unzip -q ~/Downloads/GoHome-v0.2.21.zip -d /tmp/GoHome-v0.2.21

diff -ru --exclude=.git --exclude=data \
  ~/gohome /tmp/GoHome-v0.2.21/GoHome-v0.2.21 | less
```

确认没有未归档的本地功能需要保留后，再把源码合并到原 Git 工作区。保留 `.git` 与 `data/`，不需要复制 ZIP 内的 `data/.gitkeep` 覆盖运行数据。

然后自行测试和编译：

```bash
go test ./...
go vet ./...
make
```

`make` 会按原 Git 提交总数、短哈希、最近提交说明与构建日期生成版本信息，并编译 Linux / Windows amd64。本源码 ZIP 不含二进制文件，也不含 `.git` 历史。

## 升级后验证

1. 打开小米设备管理页，等待一次状态同步。
2. 在米家 App 中关闭“小冰箱”，等待 GoHome 状态轮询。
3. 页面应显示唯一一条“开关状态：关闭”，开关按钮也应为关闭。
4. 指示灯如为开启，应显示在独立的“指示灯”属性，不影响插座开关。
5. 若小米云端本轮读取失败，页面应显示“开关状态未知”，运行日志会说明请求是否超时/取消；不会要求重登。

本次通过离线单元测试和模拟响应测试，但没有连接你的真实小米账号/插座实测。若升级后仍不正确，请提供新的“小米 MIoT 实时属性读取”日志；不要发送 serviceToken、Cookie、ssecurity、密码或验证码。
