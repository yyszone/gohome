# GoHome v0.2.27 升级说明

## 本次内容

- 加入 GoHome 专属网页图标和多尺寸桌面/移动端图标。
- 登录页、侧边栏统一使用新 Logo。
- 新增独立 `theme.css` 视觉层，更新导航栏、卡片、按钮、表单、登录页、日志和局域网发现区域的颜色与细节。
- 不改动 API、设备数据结构、品牌账号、登录凭证和控制逻辑。

## Armbian 已部署用户升级

1. 备份 `/root/gohome/data/`。
2. 在原 Git 工作区合并本次源码；不要删除原来的 `.git`、`data/`、Vault 和 Session 密钥。
3. 如果使用预编译包，在原部署目录中只替换对应架构的二进制；前端资源已嵌入二进制，因此必须同时升级 GoHome 二进制才能看到新 Logo 与样式。
4. 执行 `systemctl restart gohome`。
5. 浏览器使用 `Ctrl + F5` 强制刷新；favicon 有缓存时可关闭旧标签页后重新打开。

## 资源文件

所有网页资源均通过 `//go:embed web/*` 编入单个 GoHome 二进制，无需 Nginx、Node.js、额外静态文件目录或外网字体/CDN。

新增文件：

- `internal/app/web/favicon.svg`、`favicon.ico`、`favicon-16.png`、`favicon-32.png`、`favicon-48.png`
- `internal/app/web/gohome-logo.png`
- `internal/app/web/gohome-icon-192.png`、`gohome-icon-512.png`
- `internal/app/web/apple-touch-icon.png`
- `internal/app/web/site.webmanifest`
- `internal/app/web/theme.css`

## 资源占用说明

图片资源只在构建时嵌入程序，不会启动额外进程或定时任务。主题使用 CSS 静态渐变和轻量 hover 过渡，不引入前端框架、动画库、第三方字体或外部 CDN。浏览器下载一次后可按缓存策略复用静态资源。

## 已部署 Armbian 的最短更新方法

如果只想更新网页外观，不需要重新安装 systemd 或清空数据。使用对应 ARM 二进制包覆盖程序即可：

```bash
cd /root/gohome
cp -a gohome-linux-arm64 "gohome-linux-arm64.bak.$(date +%Y%m%d-%H%M%S)"
# 将 GoHome-v0.2.27-Armbian-binaries.zip 上传到 /root/gohome 后：
unzip -o GoHome-v0.2.27-Armbian-binaries.zip -d /root/gohome
chmod 755 gohome-linux-arm64
systemctl restart gohome
systemctl status gohome --no-pager
```

如果设备运行 ARMv7 32 位用户空间，保留/替换 `gohome-linux-armv7`；服务启动脚本会自动选择。替换二进制不需要改动 `/root/gohome/data/`。
