# GoHome v0.2.27 测试报告

## 测试项目

- `go test -count=1 ./...`
- `go test -race -count=1 ./...`
- `go vet ./...`
- `node --check internal/app/web/app.js`
- HTML 中 favicon、Logo、主题 CSS、manifest 引用路径检查
- HTTP 静态资源响应：MIME 类型、首页 no-cache、静态资源 24 小时缓存策略
- `make armbian`：完成 ARMv7 + ARM64 实际交叉编译；通过 ELF Class/Machine 验证目标架构
- 新增 PNG/ICO/SVG/manifest 文件存在性和图像尺寸检查
- `go build` 验证 `//go:embed web/*` 能正确嵌入新增静态资源
- Makefile 在无 `.git` 源码 ZIP 中版本回退值检查（`0.2.27`）
- ZIP 完整性检查与运行数据/凭证文件排除检查

## 视觉实现检查

- 登录页和侧边栏共用 GoHome 专属图标。
- SVG、ICO、PNG favicon 多格式兼容。
- Apple Touch Icon 和 192/512 Web App 图标已提供。
- 主题覆盖不变更原有交互脚本、DOM ID、API 路由或设备开关逻辑。
- 保留现有移动端 CSS 断点；新增 reduced-motion 支持。
- 所有静态资源通过 Go embed 提供，不依赖外部 CDN、Node.js 或额外服务。

## 验证边界

自动测试可以验证 Go 代码、静态资源引用和资源嵌入；未在用户魔百盒实机浏览器上进行视觉截图验证。用户升级后建议使用 `Ctrl + F5` 清除旧 CSS/Logo/favicon 缓存。
