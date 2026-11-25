# 房间名称映射与品牌账号别名

## 状态轮询与房间元数据分离

房间元数据低频变化，因此只在品牌账号首次设备同步、用户手动同步时解析，不放在活动页面的实时状态轮询中。

- eWeLink：设备记录中的 `itemData.family.roomid` 是 ID。普通设备同步额外查询 `GET /v2/family?lang=cn`，将 `data.familyList[].roomList[].id` 映射为 `name`。
- Xiaomi：旧 `xiaomiio` ServiceToken 会话使用 `/app/v2/homeroom/gethome_merged`。从 `result.homelist[].roomlist[]` 读取每个房间的 `name` 和 `dids`，得到 `did -> room_name` 映射。
- 查询失败属于可选元数据失败，不影响设备发现、登录和开关状态读取。已有房间名不会被“未分区”覆盖。

## 账号显示别名

`Account.DisplayName` 是 GoHome 本地 UI 字段。登录身份继续由 `ID`、`Provider`、`Label` 与 Vault 中的加密凭证确定。修改显示别名只调用 `Store.updateAccountDisplayName`，不会写入 Vault。

API：

```http
PATCH /api/accounts/{id}
{"displayName":"楼上米家"}
```

空字符串表示删除自定义别名，UI 回退显示原始 `Label`。
