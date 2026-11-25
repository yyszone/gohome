# GoHome v0.2.25 升级说明

## 本次修复

1. 小米房间名称不再只依赖 `gethome_merged` 的单一 `homelist.roomlist` 结构。
2. 增加共享家庭、家庭级设备、预合并 `devices[did]` 映射、数字/字符串 DID 兼容。
3. `gethome_merged` 只有部分设备匹配时，也会回退到官方 Xiaomi Home 集成的 `gethome` + `get_dev_room_page`；分页返回的家庭/房间 ID 会关联回首页的房间中文名。
4. 小米房间元数据接口异常会记录到“运行日志”，但不会中断设备清单同步或清除已登录账号。
5. 小米主开关、eWeLink 单路/多路开关、MIoT 属性和动作补齐成功/失败操作日志；eWeLink Token 刷新和控制失败也会记录。
6. 设备操作成功或失败后，网页自动刷新本地运行日志列表。

## 升级方法

在 NAS 原项目目录执行，先备份运行数据：

```bash
cd ~/gohome
cp -a data "data.backup-$(date +%Y%m%d-%H%M%S)"
mkdir -p /tmp/GoHome-v0.2.25
unzip -q ~/Downloads/GoHome-v0.2.25.zip -d /tmp/GoHome-v0.2.25

diff -ru --exclude=.git --exclude=data \
  . /tmp/GoHome-v0.2.25/GoHome-v0.2.25 | less
```

比较后将源码合并到原 Git 工作区，不要覆盖 `.git/`、`data/`、Vault 或 Session 密钥。保留原工作区的 Git 历史，再执行：

```bash
go test ./...
go vet ./...
make
```

无需重新登录小米或 eWeLink。升级后进入“品牌接入”，对小米账号点击一次“同步”，然后到“运行日志”查看本次房间元数据结果。如果仍未分区，日志会区分“接口查询失败”和“接口返回成功但 DID 没有房间关联”。

## 注意

本版本使用 HA 实际采用的 Xiaomi 云端接口和数据结构实现兼容，但仍需用户在真实米家账号上点击同步验证云端是否为这些设备返回了房间关联。若日志明确显示接口成功但 DID 未匹配，优先检查米家 App 中设备是否已加入家庭并分配房间。
