# 源码基线说明

GoHome v0.2.18 是在此前交付的 GoHome v0.2.17 源码 ZIP 上继续修改的。

此前 v0.2.17 的 `SOURCE_BASELINE.md` 已披露：当时无法取得完整 v0.2.16 源码 ZIP，只能基于 v0.2.15 完整源码做后续修改。当前 v0.2.18 因此继承相同的来源限制，不应被误认为自动包含用户本地 v0.2.16 或 v0.2.17 工作区中未进入交付 ZIP 的额外改动。

本版新增/修改文件主要包括：
- `internal/app/page_activity.go`
- `internal/app/provider_state_poller.go`（接口流程沿用，轮询时间常量在 page_activity 中调整）
- `internal/app/providers_xiaomi_miot.go`
- `internal/app/providers_ewelink.go`
- `internal/app/main.go`
- `internal/app/energy_sampler.go`
- `internal/app/main_test.go`
- `internal/app/web/app.js`

部署前请对照此说明检查本地是否存在未归档的私有修改。
