# GoHome 源码模块与后续 Patch 维护

本目录是 GoHome v0.2.29 源码基线。建议后续功能继续按职责拆分到独立文件，避免把认证、设备语义、品牌协议和前端页面逻辑集中堆到 `main.go`。

## 目录职责

| 路径 | 职责 | 适合修改的内容 |
| --- | --- | --- |
| `cmd/gohome/` | 程序入口 | 启动参数、信号处理的入口编排 |
| `internal/app/main.go` | App、Store、HTTP 路由与 API | 设备数据结构、通用设备 CRUD、路由注册 |
| `internal/app/auth.go` | Cookie 会话 | 登录、登出、签名 Cookie 和会话校验 |
| `internal/app/admin_password.go` | 管理员密码生命周期 | 首次随机密码、6–128 字符密码校验、密码文件权限、修改密码、会话密钥轮换 |
| `internal/app/camera.go` | 摄像头地址校验 | 浏览器视频源 URL 的协议和长度校验 |
| `internal/app/device_semantics.go` | 跨品牌设备语义 | 扫地机器人等特殊设备识别和展示数据归一化 |
| `internal/app/providers_xiaomi_auth.go` | 小米认证 | 登录、验证码、二次验证、凭证保存 |
| `internal/app/providers_xiaomi_miot.go` | 小米 MIoT | 设备规格、属性读取、属性写入和动作调用 |
| `internal/app/providers_ewelink.go` | eWeLink 接入 | 普通账号认证、设备同步和设备控制 |
| `internal/app/web/index.html` | 页面骨架和弹窗 | 页面 DOM、模态框和静态资源引用 |
| `internal/app/web/app.js` | 前端交互 | 设备列表、状态渲染、事件委托和 API 调用 |
| `internal/app/web/app.css` | 结构和响应式样式 | 布局、组件和移动端适配 |
| `internal/app/web/theme.css` | 品牌主题覆盖 | 色彩、视觉效果；尽量不在这里写布局逻辑 |
| `scripts/service-start.sh` | systemd 二进制选择 | 按实际用户空间架构选择根目录下的二进制 |
| `scripts/deploy.sh` | 远程发布 | SSH 连接复用、架构校验、目录创建、上传、原子替换和失败回滚 |
| `Makefile` | 测试、构建、部署编排 | 跨平台构建目标、DEPLOY_PATH 和部署参数 |

## 本地开发和检查

```bash
# 完整测试和静态检查
go test ./...
go test -race ./...
go vet ./...

# 前端语法检查（需要 Node.js；仅检查，不参与构建）
node --check internal/app/web/app.js

# 交叉编译 Armbian ARM32 + ARM64
make armbian
```

生产构建不依赖 Node.js，也不使用 Docker；网页资源通过 `//go:embed web/*` 编入 Go 可执行文件。

## 生成后续 Patch

建议先建立 Git 仓库并提交一个清晰的基线，再按单个功能提交变更：

```bash
git init
git add .
git commit -m "baseline: GoHome v0.2.29"

# 修改某个功能后，只导出对应模块的 patch
git diff -- internal/app/device_semantics.go internal/app/providers_xiaomi_miot.go > robot-state.patch

# 或将一个已提交功能导出成可 git am 的邮件补丁
git format-patch -1 HEAD --stdout > feature.patch
```

应用普通 diff 补丁前先检查：

```bash
git apply --check feature.patch
git apply feature.patch
```

如果目标文件有本地未提交修改，先保存 `git diff --binary > ../before.patch`，不要使用 `git reset --hard`、`git checkout --` 或强制覆盖。Patch 应尽量只触及该功能所属的模块和对应测试文件。

## 版本约定

- 源码 ZIP 不携带 `.git`，因此 `Makefile` 的 `SOURCE_VERSION` 是源码构建的版本来源。当前归档版本为 `0.2.29`。
- 前端静态资源统一使用 `?v=0.2.29`，发布前更新版本时同步修改 `index.html` 和 `site.webmanifest`。
- 新增 Go 逻辑时同步新增单元测试；设备协议分类应以规格描述、单位和服务元数据为依据，避免只根据单个属性名猜测。
- `data/` 是运行数据目录，不应放入源码 ZIP；不能将 `gohome.json`、Vault 密钥、Session 密钥或管理员密码文件提交到 Git。

## 摄像头预览边界

网页预览要求摄像头或视频网关提供浏览器可以访问的 HTTP/HTTPS 视频源。当前前端可直接预览 MJPEG、MP4、WebM；HLS (`.m3u8`) 取决于浏览器原生支持。RTSP、小米私有 P2P 和需要厂商 App 解密的流不能直接塞进 HTML `<video>`。这类设备需要先在局域网运行 go2rtc / MediaMTX 等网关，将真实支持的源转换成浏览器支持的流，再把 HTTP 地址填入 GoHome。GoHome 本身不伪造摄像头流，也不会自动获取小米摄像头的私有视频凭证。
