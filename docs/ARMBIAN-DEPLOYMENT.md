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


## 一键远程部署（开发机执行）

默认项目目录为 `/root/gohome`：

```bash
make deploy DEPLOY_HOST=root@192.168.31.63 DEPLOY_ARCH=arm64
```

也可以指定其他绝对目录，例如：

```bash
make deploy DEPLOY_HOST=root@192.168.31.63 DEPLOY_ARCH=arm64 DEPLOY_PATH=/opt/services/gohome
```

`DEPLOY_PATH` 是远程 GoHome 项目根目录，默认 `/root/gohome`。首次使用自定义目录时，先将完整项目目录（包含 `scripts/`、`deploy/` 和架构二进制）放到目标目录，再在目标设备上运行：

```bash
cd /opt/services/gohome
sudo sh scripts/install-systemd.sh
```

这一步会生成指向当前目录的 `gohome.service`。之后 `DEPLOY_PATH` 必须与 systemd 服务实际使用的项目目录一致。部署脚本会自动创建目录，但不会自动迁移已有服务或复制完整源码。

SSH 密码登录时，部署脚本会先建立 SSH ControlMaster 主连接；架构检查、目录创建、SCP 上传、二进制切换和 systemd 重启复用该连接，因此一次部署只需输入一次 SSH 密码。脚本结束时会关闭临时连接。
