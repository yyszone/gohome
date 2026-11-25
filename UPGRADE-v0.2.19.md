# GoHome v0.2.19 升级说明

## 本版修复

小米旧 `xiaomiio` 会话的实时属性请求此前把多个 DID 合并进一个 `prop/get` 请求，并且解析时强制要求响应行包含 DID。部分旧云端响应只返回 `siid/piid/code/value`，不重复返回 DID，导致数据全部被忽略。

v0.2.19 改为每台设备独立请求：一台设备的多个属性仍然合并；最多 3 台设备并发。单设备请求允许响应省略 DID，但只要响应显式带了其他设备的 DID 就拒绝应用。读取失败会在日志里给出型号、规格或业务响应原因。

## 登录与其他品牌

- 不修改小米登录、验证码、serviceToken、ssecurity 或 Cookie。
- 不修改 eWeLink 登录和控制代码。
- 不要求重新登录。保留整个 `data/` 目录及 `gohome.vault.*`、`gohome.session.key`。
- 本版没有实现小米新版 OAuth2/MQTT 推送；旧账号 serviceToken 不能当作 OAuth2 Bearer 使用。

## 合并到现有 Git 工作区

建议先在新目录解压并比较：

```bash
mkdir -p /tmp/GoHome-v0.2.19
unzip -q ~/Downloads/GoHome-v0.2.19.zip -d /tmp/GoHome-v0.2.19

diff -ru --exclude=.git --exclude=data ~/gohome /tmp/GoHome-v0.2.19/GoHome-v0.2.19 | less
```

确认没有未归档的本地修改后，再合并源码。不要覆盖旧 `data/`、`.git/` 或密钥文件。新包包含 `.gitignore`，也保留 `data/.gitkeep`。

## 自己测试和编译

```bash
cd ~/gohome
go test ./...
go test -race ./...
go vet ./...
make
```

`make` 根据当前 Git 提交总数注入动态版本号并编译 Linux / Windows x64。本 ZIP 不包含二进制文件，也不包含 `.git` 历史。
