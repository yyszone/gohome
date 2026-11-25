# 小米房间名称同步说明

GoHome 的设备主列表接口 `/app/home/device_list` 不保证直接提供 `room_name`。房间名称必须由家庭/房间元数据中的 DID 关联补齐。

## 主路径

优先调用 `POST /app/v2/homeroom/gethome_merged`，按 Home Assistant `hass-xiaomi-miot` 的方式解析：

- 遍历 `result.homelist[].roomlist[]`。
- 对每个房间读取 `name` 和 `dids[]`，建立 `did -> room_name` 映射。
- 同时处理 `share_home_list`，并兼容 `result.devices[did].room_name`。
- DID 统一转换成字符串，避免 JSON 数字 DID 导致整个结构反序列化失败。

## 备用路径

如果主路径失败或没有建立任何 DID 映射，则使用官方 Xiaomi Home 集成采用的接口流程：

1. `POST /app/v2/homeroom/gethome` 获取家庭列表。
2. 解析主家庭和共享家庭的 `dids`、`roomlist[].id/name/dids`，既保留房间名，也建立 `home_id -> room_id -> room_name` 索引。
3. 即使 `gethome_merged` 已经匹配到部分设备，也会继续用 `gethome` 补齐未匹配的 DID；只有所有待补充分区设备都有名称时才跳过备用请求。
4. 若 `has_more` 为 true，使用 `max_id` 调用 `POST /app/v2/homeroom/get_dev_room_page`，逐页读取 DID 和房间 ID，再通过首页保存的 `home_id / room_id` 还原中文房间名称。分页数据本身可能不带房间名称，不能直接把 `roomlist[].id` 当显示名称。

## 同步和日志策略

- 房间查询只在登录后的初始设备同步或用户手动点击“同步”时执行。
- 页面活跃期间的 10 秒状态轮询不重复请求家庭/房间接口。
- 房间接口失败不会阻断设备列表同步，也不会删除品牌登录凭证。
- 分页查询失败时，已获取到的房间映射仍会保留，同时在运行日志报告错误原因。
- 设备仍显示“未分区”时，运行日志会报告具体接口错误；若接口成功但 DID 未映射，则提示可能未在米家 App 中分配房间。

## 参考实现

- `https://github.com/al-one/hass-xiaomi-miot/blob/master/custom_components/xiaomi_miot/core/xiaomi_cloud.py`
- `https://github.com/XiaoMi/ha_xiaomi_home/blob/main/custom_components/xiaomi_home/miot/miot_cloud.py`
