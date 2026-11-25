# GoHome v0.2.24 测试报告

## 已执行检查

- `go test ./...`
- `go test -race -count=1 ./...`
- `go vet ./...`
- `node --check internal/app/web/app.js`
- `make -n`（仅查看构建命令，不实际编译）
- ZIP 完整性检查及文件清单检查

## 新增 / 覆盖的回归点

- 没有页面租约时调度器返回 idle；页面 lease 创建后唤醒并立即安排首次轮询；释放 lease 后停止调度。
- eWeLink `/v2/family` 的房间 ID 到中文名称映射。
- eWeLink 实时状态刷新不会请求额外的 `/v2/family` 房间接口。
- 小米 `gethome_merged` 的 `dids -> room_name` 映射。
- 房间查询未返回名称时保留旧的已知房间；新名称返回时可更新。
- 账号重命名只修改 `DisplayName`，不改变账号 ID、Provider、原始 Label、Region、Status。
- 普通账号状态更新不会覆盖自定义显示名称；清空显示名称恢复原始标签。

## 边界

本报告是 Go 单元/并发测试与前端语法检查结果，不代表已使用用户的真实小米/eWeLink账号执行云端实机验证。房间接口在真实账号上的返回情况仍需升级后手动同步确认。本版本没有编译 Linux/Windows 可执行文件。
