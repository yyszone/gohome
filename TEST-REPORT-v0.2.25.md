# GoHome v0.2.25 测试报告

## 已执行检查

- `go test -count=1 ./...`
- `go test -race -count=1 ./...`
- `go vet ./...`
- `node --check internal/app/web/app.js`
- `make`（测试、静态检查及 Linux/Windows amd64 交叉编译）
- ZIP 完整性检查

## 新增回归覆盖

- 小米房间解析支持 `result.devices[did].room_name`。
- 小米房间解析支持主家庭与共享家庭 `roomlist[].dids`。
- 小米 DID 支持 JSON 字符串和数字。
- 小米官方 `get_dev_room_page` 分页解析及 `max_id/has_more`。
- 小米房间同步未匹配时写入可查看的运行日志；设备仍然正常导入。
- 小米主开关控制成功后写入操作日志。
- eWeLink 单路/多路开关成功和失败路径写入操作日志。
- MIoT 属性/动作成功和失败路径保留操作日志。
- 前端控制成功或失败后重新读取运行日志；MIoT 下拉属性控制同样刷新。

## 实机验证边界

测试均为本地单元测试、并发检查、静态检查和交叉编译；没有拿用户的真实小米/eWeLink 凭证访问云端，也没有对实际插座发出控制命令。升级后需使用真实小米账号执行一次手动同步，才能确认米家云端是否确实返回了这两台设备的房间 DID 映射。
