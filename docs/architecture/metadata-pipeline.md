# 阶段 03：资料、任务、人物与图片后端交接

日期：2026-10-02（Asia/Shanghai）。对应 [03](../../.scratch/architecture-refactor/issues/03-metadata-providers-assets.md)。配置语义与首轮验证仍见 [配置基础历史记录](./provider-settings.md)。本文描述新 runtime 的当前能力，不把旧 UI 回归当作新前端完成证据。

## 模块与启动

- `internal/metadata/providers`：`Adapter` / `Capabilities` / `Search` / `FetchTitle` / `FetchImage`；纯上游协议，无 store 依赖、站内路由或 MetaTube 整数映射。共享值在 `internal/model/metadata.go`。
- `metadata.New` 解密并发布配置，建立应用生命周期；`Start` 恢复非破坏性任务并启动两个 worker；`Close` 拒绝新任务、取消网络并等待 worker/共享图片任务退出。`app.NewRuntime` 已装配 Start/Close。
- `NewWithOptions` 只提供 HTTP RoundTripper 测试 seam；生产 runtime 不开放 TMDB/TVDB origin 覆盖。测试仍经过真实 `httptest` HTTP 与临时 SQLite。
- worker 每 200 ms 查询有界待办领取；不会扫描媒体目录，不把全部媒体/资料读入内存。一个目标最多 65 秒、一个来源最多 20 秒；API 请求起始间隔 250 ms，来源独立，429/鉴权/5xx 最少冷却一分钟，尊重更长 Retry-After。
- 结构化日志只记录 subject ID、generation、完成/失败/失效类别；不记录令牌、响应正文或媒体标题。

## 来源行为

| 来源 | 关键行为 |
| --- | --- |
| TMDB | 字符串远端 ID；电影/电视剧搜索；zh-CN 基本资料，缺项 en-US/原文回退；电影 credits、电视剧 aggregate_credits；ID 校验；横图按类型、比例、语言选择 |
| TVDB | API Key/PIN 登录，缓存 JWT 最多 24 小时；401 丢弃 JWT，不无限重试；搜索不设置过滤原语言的 language=zho；中文→英文→原名；按已知 peopleType 建立演员/导演，按 artwork types 选择横图 |
| MetaTube | 仅电影；provider 小写、remote ID 不变；搜索只读；一次详情取得基本资料/演职员/图片引用；name-only 人物不猜外部 ID；图片地址只由配置前缀和文档端点构造，不访问返回的任意图片 URL |

独立演员查询（2026-10-02）补充：`SearchActors/FetchActor/FetchActorImage` 通过 MetaTube 演员接口工作，不改变上表电影演职员的 name-only 身份规则。`ActorRef` 与电影 `RemoteRef` 分开，按姓名得到候选后由用户选择，不写本地人物/作品关联。Service 签发一小时、配置版本绑定且用途隔离的演员/图片引用；家庭鉴权仍由 transport 执行。在途配置变更后丢弃旧结果，客户端不得传任意 URL。

这条独立查询路径不进入作品快照/持久化 asset 缓存，也不创建数据库记录；详情按需使用上游 `lazy=true`。头像和壁纸只请求已配置 MetaTube 的 `images/primary`；壁纸的 `ratio=0` 及 `url` 仅由经校验的演员详情 images 和服务端签名确定，ReelNest 不访问图片源站。最多 50 候选、20 张壁纸、1 MiB 上游 JSON / 5 MiB 单图；沿用 API/图片 Gate 与下载并发预算。原有 `/api/assets` 缓存合并下载语义不变。

所有搜索在结果被截断、丢弃无效行、重复或分页未完时不能证明唯一。自动匹配按媒体库顺序，只接受完整候选集中唯一的规范标题/原名/编号及年份匹配；无可靠结果继续回退。文件路径先取文件名并去视频扩展名，避免拿绝对路径或扩展名搜索。

配置保存不获取网络锁。领取前比较 DB revision 与不可变注册表，防止“设置提交后、注册表发布前”的间隙混用版本。Token 更新不换目录空间；MetaTube 换地址不重解释旧空间 ID。旧手动绑定及其有效快照仍保留，旧空间不会被新地址刷新。

## 持久化与版本条件提交

业务模块不拿 SQL/事务。新增 store 用例包括：

- `MetadataTarget`、`ClaimMetadata`、`BeginSelection`、`CompleteMetadata`、`RecoverMetadata`、`RefreshMetadata`、`MetadataStatus`。
- `MetadataMatch`、`CachedTitle`：按目标/远端身份查询；有效性显式检查 `expires_at > now`。
- `MetadataPerson`、`MetadataPersonWorks`、`MetadataAsset`、`CompleteAsset`。

`CompleteMetadata` 在同一事务检查目标存在/可用/类型、subject revision、binding revision、library policy revision、捕获来源的配置 revision/catalog、requested/claimed generation。成功、失败与补充资料使用同一个入口。实际来源集合之外的配置可能保守地使当前自动任务失效，但不会授权旧结果。

- 扫描移除目标由 FK 级联清除任务与绑定；删除不可用目标会递增 generation 并 supersede 相关任务。新配置/策略只排队仍可用目标。
- 手动选择先验证作品再提交；每次成功选择递增 binding revision。失败只记录尝试，不清空旧选择、不降级 automatic。
- 旧 generation 无权清除较新队列；同 generation 版本失效且目标仍可用时用新 generation 重新排队，目标不可用时 superseded。
- 每 generation 最多三次尝试，重启不退还已消耗次数。失败和待补充资料只重试已经存在且到期的任务；耗尽后等待显式刷新或相关配置变化。
- 内容/演职员的约束或 FK 写入失败整体回滚；engine 尝试以相同条件提交记录 DB 失败，数据库整体不可用时保留可重启恢复的任务。
- 当前 schema 的 `revisions` 增加独立 `metadata` 计数，供状态分页使用。任务领取/状态变化不额外使普通 catalog cursor 失效；资料与匹配尝试结果提交则同时修改 catalog revision。扫描、配置、目录策略与删除同步更新相关 revision。

这是**单一空目录初始化 schema** 的继续开发，不新增迁移或旧 DDL 兼容。已有开发阶段旧 schema hash 的 DATA_DIR 仍按既定规则拒绝启动；没有打开或转换用户数据。

## 快照、补充内容与人物

快照结构化存储，不保存完整资料 JSON 再维护另一套人物索引。只有角色字符串数组使用局部 JSON。

- 有效基本资料优先从缓存读取；只有到期的基本资料才重新获取。
- credits/artwork 独立状态和 retry_at；只请求 missing 或到期 failed 的部分。补充提交校验 snapshot revision，**不延长原 fetched_at/expires_at**；基本资料成功可以伴随可选内容失败。
- 有可靠人物 ID：key 带来源目录空间；只有姓名：key 限定当前作品及关系，不跨作品按同名合并。
- 人物及作品只读当前绑定、未过期快照与本地目标。每个远端作品只选一个本地代表，优先可用版本；电视剧用系列卡，不重复列出单集。
- 人物作品有 50 默认/100 最大的 keyset 页，cursor 绑定人物、页大小、catalog/favorites revision 和最早相关 TTL，使用独立 HMAC 域。当前页装配是有界基础实现，**不宣称达到阶段 04 的 SQL 次数/50,000 文件性能目标**。

## 统一图片

- 匹配只存受控引用；Primary/Thumb/Backdrop/photo 全部经过统一按需缓存。
- singleflight key 含 asset、引用版本、快照版本、配置版本。全局最多四个、每来源最多两个下载；命中直接读 SQLite，不等其它慢图。
- 共享下载使用应用生命周期和 15 秒预算。每个等待者可以独立取消，取消页面不写负缓存；真实上游失败默认退避 24 小时。
- API 和图片独立通道/限流；图片没有 API Authorization。拒绝重定向、HTML/SVG、MIME/签名不符和超过 5 MiB 的图片；JSON 最大 1 MiB。
- 写回核对受控引用、asset/snapshot/provider revision、当前目录空间、资料 TTL、当前可用本地绑定。改绑、扫描移除、文件删除、配置变化后的旧字节或旧负缓存不能写回。
- 图片 TTL 不超过资料 TTL 和 30 天来源上限。旧 MetaTube 空间已缓存的有效图片可读至原 TTL，但不能发新下载。

## 已挂载的新 HTTP 契约

除既有设置 API 外，均受家庭会话、请求 OpenAPI 校验和写入同源校验保护：

- `GET /api/metadata/subjects/{id}/candidates`（source/q）
- `PUT /api/metadata/subjects/{id}/selection`
- `POST /api/metadata/refresh`、`GET /api/metadata/status`
- `GET /api/people/{id}`、`GET /api/people/{id}/works`
- `GET/HEAD /api/assets/{id}/content`

显式 HTTP DTO，不直接序列化数据库行或凭据。成功/错误响应与现有 OpenAPI 自动校验一致；没有新旧路由 alias。阶段 04 可复用 Match/人物查询，阶段 05 可消费这些接口。

## 验证与未验证边界

本次补充 22 个顶层测试，包含更多参数化子场景；连同配置增量 14 项及已有基础测试，覆盖协议、完整性、错误脱敏、FK 原子回滚、全部版本维度、补充 TTL、名称/目录空间隔离、人物分页、任务重启、下载并发/取消/改绑/移除/删除，以及新 HTTP 请求和响应契约。实际命令与结果以 issue 的 Answer 为准。

性能采样是小型合成数据、与其他测试并行运行的本机结果，不是生产 SLA。没有修改 CLI/旧前端入口，没有部署、重启用户服务、真实资料源/NAS/播放器验证或用户媒体操作。Docker CLI 不可用。27 个 Playwright 场景仍属于旧 UI 安全/行为回归；新 UI、查询性能和旧实现删除分别属于 05、04、06。
