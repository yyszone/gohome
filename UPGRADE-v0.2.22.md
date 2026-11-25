# GoHome v0.2.22 升级说明

## 本版改动

- M3 的指示灯 (SIID 3 / PIID 1) 仍显示状态，但不会再渲染单独的“开启”属性按钮。
- 小米主继电器点击后，页面立即乐观更新；命令成功后服务端立即保存并返回目标状态，不再同步等待额外的 MIoT 读取。
- 后端约 700ms 后使用独立后台 Context 读回主继电器，读取成功就校正真实状态。浏览器切换页面或关闭连接不会取消该读回。
- 同一个设备的多个连续控制使用命令序号保护，旧的读回不能覆盖新命令。
- 前端点击时立即切换按钮外观；请求成功合并服务器设备快照，失败则还原状态。
- eWeLink 原有控制路径不变，同时修复单通道设备的设备级 `power` 与 channel 状态同步。

## 升级方式

保留原 `.git`、`data/` 和登录密钥，先解压比较源码，再合并本次变更：

```bash
cd ~/gohome
mkdir -p /tmp/GoHome-v0.2.22
unzip -q ~/Downloads/GoHome-v0.2.22.zip -d /tmp/GoHome-v0.2.22
diff -ru --exclude=.git --exclude=data . /tmp/GoHome-v0.2.22/GoHome-v0.2.22 | less
```

确认后在原 Git 工作区执行：

```bash
gofmt -w internal/app/main.go internal/app/main_test.go
go test ./...
go test -race ./...
go vet ./...
make
```

本 ZIP 不包含 `.git`、真实 `data/`、Vault/Session 密钥或二进制文件。无需重新登录小米或易微联。

## 重要说明

小米控制命令采用快速乐观反馈：云端接受 `prop/set` 后立即显示目标状态，随后后台独立读取真实属性校正。如果命令写入被云端拒绝，API 会返回错误，网页恢复原状态；如果后台读回暂时失败，会记录日志并暂时保留已被云端接受的命令状态，下一轮页面活跃同步会再次校准。
