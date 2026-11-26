# GoHome v0.2.29 升级说明

本版本在 v0.2.28 基础上优化远程部署连接和管理员密码设置。

## 更新内容

1. **SSH 密码只输入一次**：部署脚本先建立 SSH ControlMaster 主连接；后续架构检查、目录创建、SCP 文件上传、原子替换和 systemd 重启都复用这条连接。连接使用私有临时控制套接字，部署结束自动关闭并清理。
2. **支持自定义远程目录**：`DEPLOY_PATH` 默认为 `/root/gohome`，也可以设置成 `/opt/services/gohome` 等绝对路径。脚本会自动创建指定目录。
3. **管理员密码允许 6 位**：现在允许 6–128 个字符，例如 `123456`。首次生成的随机密码仍为 48 位，不会因升级自动变短。修改密码仍需验证当前密码并两次输入一致；修改后会轮换 Session 密钥，使其他浏览器的旧 Cookie 失效。

## 默认目录部署

```bash
make deploy DEPLOY_HOST=root@192.168.31.63 DEPLOY_ARCH=arm64
```

默认目录为 `/root/gohome`，默认服务名为 `gohome`。支持的架构为 `arm64`、`armv7`、`amd64`。

## 自定义目录部署

例如将项目部署到 `/opt/services/gohome`：

```bash
make deploy DEPLOY_HOST=root@192.168.31.63 DEPLOY_ARCH=arm64 DEPLOY_PATH=/opt/services/gohome
```

**重要：** `DEPLOY_PATH` 必须与 `gohome.service` 中的 `WorkingDirectory` 和 `ExecStart` 路径一致。首次改用自定义目录时，先将完整 GoHome 项目目录放到该位置（不能只放二进制），然后在目标设备执行：

```bash
cd /opt/services/gohome
sudo sh scripts/install-systemd.sh
```

该脚本会根据自身所在目录重新生成 systemd 服务单元并启动服务。之后即可从开发机用同一个 `DEPLOY_PATH` 部署新版本。只修改 `DEPLOY_PATH` 而不迁移项目目录或更新 systemd 单元，服务仍可能从旧目录启动。

## 数据安全

部署仅替换 `scripts/service-start.sh` 对应架构的二进制，不删除 `data/`、品牌账号凭证、Session 密钥或管理员密码文件。仍建议在重大升级前备份远程 `data/`。

6 位数字密码比较容易猜测，建议仅在可信家庭局域网使用，不要把 GoHome 管理端口直接暴露到公网；更推荐使用长度更长的密码。
