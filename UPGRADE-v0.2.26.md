# GoHome v0.2.26 升级说明

## 本版内容

- 新增 Linux ARMv7 32 位、Linux ARM64 64 位交叉编译目标。
- 默认 `make` 会构建 Linux amd64、ARMv7、ARM64 和 Windows amd64；`make armbian` 只构建 Armbian 的两种 ARM 架构。
- 新增 systemd 服务模板、架构自动选择启动脚本和安装脚本。
- systemd 服务固定项目工作目录，并显式设置 `GOHOME_DATA_DIR`，防止运行目录变化导致数据写到其他地方。
- GoHome 支持接收 SIGTERM/Ctrl+C 并优雅关闭。

## Armbian 部署

1. 在开发机上运行 `make armbian`。
2. 将源码包解压到魔百盒 `/root/gohome`。
3. 将对应架构的 `dist/gohome-linux-armv7` 或 `dist/gohome-linux-arm64` 复制到 `/root/gohome/`，保留该文件名。
4. 在盒子上执行：

```bash
cd /root/gohome
chmod +x scripts/service-start.sh scripts/install-systemd.sh
bash scripts/install-systemd.sh
```

服务会根据 `uname -m` 自动选择 ARMv7 或 ARM64 二进制，并设置：

```text
WorkingDirectory=/root/gohome
GOHOME_DATA_DIR=/root/gohome/data
GOHOME_ADDR=0.0.0.0:17880
```

查看服务：

```bash
systemctl status gohome --no-pager
journalctl -u gohome -f
```

## 数据和原账号保护

本次不修改 JSON 数据格式、Vault 凭证格式、Session 签名密钥或小米/eWeLink 登录协议。保持原有 `data/` 目录即可，不需要重新登录品牌账号。

建议在替换二进制前备份：

```bash
cd /root/gohome
cp -a data "data.backup-$(date +%Y%m%d-%H%M%S)"
```

**不要删除 `data/gohome.vault.json`、`data/gohome.vault.key` 或 `data/gohome.session.key`。**
