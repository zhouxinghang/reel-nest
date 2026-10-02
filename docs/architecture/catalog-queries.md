# 查询投影与分页交接（阶段 04）

日期：2026-10-02。对应 `.scratch/architecture-refactor/issues/04-catalog-queries-performance.md`。

## 运行边界

本阶段接入 **新 schema 的 `app.NewRuntime` / `transport.New`**，未切换 `cmd/reelnest` 或旧前端，未部署、重启用户服务、迁移应用数据或读取真实媒体/凭据。旧 `internal/app` 的 `attachMetadata` 仍供尚未切换的旧界面使用，**新查询链路不调用它**；05 切换消费者，06 删除旧实现，不把中间交接宣称为最终单一路径。

## 数据与读取

- `catalog_entries` 每个媒体文件和每部电视剧各一行。已组织单集也有文件行，以支撑未匹配、独立文件收藏和续播。普通浏览/最近添加只展示电影文件、电视剧摘要、未组织电视剧文件。
- 扫描在发布事务内重建本库投影，不影响其他库；删除只更新被删文件和所属电视剧的可用计数，保留文件记录直到完整成功扫描清理。测试通过真实 SQLite trigger 拒绝无关 INSERT/DELETE/UPDATE，防止退化为全库重建。
- `metadata_snapshots` 新增 STORED generated 排序/搜索列，随标题写事务更新；绑定/快照通过索引关联到投影，不复制演职员、图片字节、收藏或进度。匹配、改选和到期均不需要逐个重写单集资料副本。
- 标题排序固定为 `golang.org/x/text v0.30.0` 的简体中文 collation、Numeric、IgnoreCase（算法 v1）。NFC + Unicode case fold 用于子串搜索；`%`、`_` 是字面量，非 SQL 通配符。依赖锁及 Go 测试固定中文/数字/大小写/组合字符行为；改变算法须同时改变函数/游标版本及 fresh schema，不能复用旧排序键。
- 搜索单集文件名通过 `entries_subject` 定向查其电视剧的文件搜索列，返回整部作品原计数。不会先过滤单集再聚合，也没有用 FTS 分词偷换子串语义。
- 单文件详情最多 **7 条 store SELECT**，HTTP 家庭会话再加 1 条；只解析该目标最多 150 条演职员。测试破坏其他 999 部作品的 characters 元素类型，目标详情仍成功。列表的共同 `readCards` 仅查询一页 keys 的轻量字段/三种图片 ID，不读 credits 或 BLOB。
- 人物作品先按远端作品去重，选择可用的本地代表，然后一次装配所选卡片、一次读取该人物在该页作品的角色/人物关系；不再每张卡另查进度、收藏、图片。人物与作品都遵守当前绑定和快照 TTL。

## 05 可直接消费的 HTTP

所有响应使用已有 OpenAPI / 生成 TS 类型，HTTP 集成测试验证每次成功/失败响应。

| Interface | 语义 |
| --- | --- |
| `GET /api/home` | 20 个媒体库入口一页（含空库），每库最多 6 张预览；全局最近添加/逐文件续播各最多 20 张 |
| `GET /api/catalog/groups` | 分类/搜索/收藏/未匹配的分组摘要；不支持 recent/continue；每组给 library、完整 total、entriesCursor |
| `GET /api/catalog?libraryId=…` | 单库条目；默认 50、最大 100；browse / unmatched / favorites / continue / recent |
| `GET /api/catalog?view=recent或continue` | 无 libraryId 的全局独立分页；不会先对媒体库各截断再合并 |
| `GET /api/media/{id}` | 文件卡、路径属性、完整目标 Match，按需获取 |
| `GET /api/series/{id}` | 电视剧卡、完整 Match、分页季摘要；不内嵌全部单集 |
| `GET /api/series/{id}/seasons/{number}/episodes` | 按集号 + 文件 ID 的独立文件版本页，不把多个版本数组静默截断 |
| `PUT /api/favorites/{media或series}/{id}` | 文件/电视剧独立收藏，设置幂等，要求家庭账号/同源保护 |
| `GET /api/people/{id}/works` | 有界人物本地作品页，复用同一卡片 DTO |

### 客户端规则

1. 全局普通浏览必须先取 groups，再以每组 `libraryId`、相同过滤条件、**相同 limit** 和 `entriesCursor` 请求组内第一页。组内后续页用自己的 `page.nextCursor`。组摘要按完整匹配集合首项排序键 + 媒体库 ID 排序，不取已加载子页重新排序。
2. 默认 title 排序；recent/favorites/continue 强制其事件时间倒序 + entryKey。普通 browse/unmatched 可按 title/recent，禁止用不存在的 favorite/progress 排序。
3. cursor 是安装密钥签名、压缩且有解压上限的 opaque token；包含 scope/过滤/limit/排序位置/revision/期限，不能编辑、跨资源或跨 DATA_DIR 复用。
4. 普通浏览、人物作品、季集、home 媒体库入口依赖 catalog revision；只有 favorites 加 favorites revision、continue 加 progress revision。卡上收藏/进度可以读最新快照，但不因此使普通浏览分页持续失效。变更完成后客户端仍须精准更新相关卡片。
5. TTL 最多 5 分钟，并不跨越相关作用域最近有效资料到期。库内页保守采用本库全部当前绑定的最近到期点（包括当前被过滤的条目）；全局页采用全局范围。否则 fresh 条目到期进入 unmatched 会造成漏项。续页不续期；返回前重新检查期限，失效返回 `409 CURSOR_STALE`，清空该页 cursor 后重取。
6. revision、期限、keyset、条目和分组计数在同一只读事务读取；不承诺跨数据变更的分页快照。静态数据无重复/遗漏，数据变更以显式 stale 恢复。
7. 前端不能保留全库数组、重新聚合/排序、无限累积 DOM。05 继续负责 URL、页面/窗口化、Abort、Query cache 与交互，不在本阶段伪称新版浏览器全链路已完成。

## 可复跑性能与限制

命令（报告路径须是绝对路径）：

```sh
go test ./internal/store -run '^TestCatalogHTTPPerformance$' -count=1 -timeout 15m -v \
  -args -catalog-http-report=/absolute/path/catalog-http.json
```

只创建临时 SQLite 合成样本：10 库，1,000 / 10,000 / 50,000 文件，分别 1,000 / 5,000 / 5,000 资料目标，目标含 100 演员 + 50 导演。使用真实 loopback HTTP、家庭会话鉴权、请求契约校验及 JSON 序列化，无 mock 业务查询或数据库。

- 每个 endpoint 独立重开 SQLite 后第一次请求单列为 firstRead；**没有清 OS 页缓存，不能称为 OS 冷盘性能**。之后连续 30 次热请求分别记录 p50/p95。
- 报告包含硬件/Go/Node、样本生成耗时、真实响应字节/返回行数、实际查询 SQL/EXPLAIN、分页前匹配行数、详情 SELECT 数。匹配行/返回行是准确计数，未插桩 SQLite VM 的实际访问行数，不把二者混称。
- 单库查询使用 library 索引，资料/状态使用键查找；子串、跨库分组和动态有效标题排序仍有轻量扫描/临时排序。B-tree 不能优化任意子串，未声称已消除所有扫描或达到 O(page size)。这里的界限是仅装配所选卡片和目标详情，不是掩盖数据库计算量。
- 性能数据、最终验证及未验证环境见本 issue 的 Answer；原始报告在 `docs/architecture/catalog-http-baseline.json`。这些是隔离本机工程基准，不是生产 SLA。

### 2026-10-02 最终实测

硬件与版本详见 JSON 的 environment；每档每个 endpoint 热样本 30 次。最终测量与旧 UI Playwright / 全量 Go race 验证部分重叠，报告已注明，不宣称独占机器的 SLA。下表均为真实 HTTP 热 p95（毫秒）：

| 文件数 | 首页 | 单库 50 条 | 分组摘要 | 全局最近添加 | 单文件详情 | 人物 50 部作品 |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 1000 | 6.410 | 1.993 | 7.091 | 2.277 | 1.329 | 4.792 |
| 10000 | 22.950 | 3.893 | 32.440 | 7.831 | 1.017 | 18.234 |
| 50000 | 101.035 | 8.182 | 75.853 | 26.599 | 1.078 | 17.817 |

50,000 文件档：首页 54,471 B，50 条单库列表 25,482 B，50 条人物作品 27,900 B；单文件详情 7 条 store SELECT、只读取本目标 150 条演职员。所有场景热 p95 均低于 200 ms，首页/50 条列表均低于 256 KiB。冷启动后的首次请求及其余场景完整数值保存在 JSON，不能与热样本混合。

**验证中发现并修复的退化**：第一轮样本通过，但复跑“已建立统计信息→批量刷新资料”的样本时，10,000 文件档首页/全局最近添加热 p95 分别退化到 1,057.644 / 1,089.449 ms。独立的真实 SQLite 最小复现仍约 1 秒，排除浏览器并发；`ANALYZE` 单独不能解决。EXPLAIN 与拆段耗时定位到事件排序 selection 中无用的绑定/快照 JOIN（某计划采用 expires_at 范围查找）。不再依赖 SQLite 自动消除无用 JOIN：selection 只连接过滤/排序需要的关系，所选页再装配资料/家庭状态。相同刷新样本的最小复现下降到约 22 ms（首页）/7 ms（最近添加），最终完整 HTTP 基准如上。`TestCatalogEventSelectionOmitsUnusedRelations` 防止无用 JOIN 回归，基准保留批量刷新模式和 200 ms 实际门槛；临时诊断代码已删除。
