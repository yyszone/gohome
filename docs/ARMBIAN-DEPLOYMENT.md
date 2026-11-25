# GoHome 在魔百盒 / Armbian 上部署

## 选择二进制

在设备上执行：

```bash
uname -m
getconf LONG_BIT
```

- `aarch64` + `64`：`gohome-linux-arm64`
- `aarch64` + `32`：`gohome-linux-armv7`（64 位内核运行 32 位用户空间）
- `armv7l` + `32`：`gohome-linux-armv7`

默认 ARM32 是 ARMv7（`GOARM=7`），不适用于 ARMv6。

## 从开发机编译

```bash
make armbian
```

生成：

```text
dist/gohome-linux-armv7
dist/gohome-linux-arm64
```

## 复制到盒子

把 GoHome 源码 ZIP 解压到 `/root/gohome`，再把匹配架构的二进制放到 `/root/gohome/`。确认 `Makefile`、`scripts/`、`deploy/` 直接在该目录下。

例如开发机可使用：

```bash
scp dist/gohome-linux-arm64 root@ARMBIAN_IP:/root/gohome/
# 32 位系统改为复制 dist/gohome-linux-armv7
```

## 安装 systemd 服务

```bash
cd /root/gohome
chmod +x scripts/service-start.sh scripts/install-systemd.sh
bash scripts/install-systemd.sh
```

安装脚本会：

1. 检查当前架构并确认对应二进制存在。
2. 设置二进制执行权限，创建 `/root/gohome/data`。
3. 安装 `/etc/systemd/system/gohome.service`。
4. 启用开机自启并启动服务。

服务运行参数：

```text
WorkingDirectory=/root/gohome
GOHOME_ADDR=0.0.0.0:17880
GOHOME_DATA_DIR=/root/gohome/data
```

状态和日志：

```bash
systemctl status gohome --no-pager
journalctl -u gohome -n 100 --no-pager
journalctl -u gohome -f
```

管理命令：

```bash
systemctl restart gohome
systemctl stop gohome
systemctl start gohome
systemctl disable --now gohome
```

## 更新程序

先备份 `data/`，停止服务，再替换二进制；文件名保持不变：

```bash
cd /root/gohome
cp -a data "data.backup-$(date +%Y%m%d-%H%M%S)"
systemctl stop gohome
# 将新的 ARMv7 或 ARM64 二进制复制到这里
chmod 0755 gohome-linux-arm64  # 32 位系统使用 gohome-linux-armv7
systemctl start gohome
journalctl -u gohome -n 50 --no-pager
```

不要删除 `data/gohome.vault.json`、`data/gohome.vault.key` 和 `data/gohome.session.key`，它们保存品牌账号凭证及 Web 登录密钥。

## 守护与资源使用

- systemd 在开机且网络就绪后启动 GoHome。
- 程序异常退出时 3 秒后自动重启，正常停止不会触发重启。
- SIGTERM 触发后台状态轮询取消和 HTTP 优雅关闭。
- GoHome 直接作为 systemd 主进程运行；启动脚本通过 `exec` 替换自身，不会额外保留 shell 常驻。
- 不设置 CPU/内存硬限制，不新增轮询守护进程；没有活动设备页面时，现有状态调度器休眠等待事件。
- 日志写入 systemd journal。只建议在可信家庭局域网访问，不要把 17880 端口直接暴露到公网。
