# Reel Nest HTTP API（当前运行契约）

日期：2026-10-02。规范源为 [`api/openapi.yaml`](../api/openapi.yaml)，对应生成类型 [`web/src/generated/api.d.ts`](../web/src/generated/api.d.ts)。API 与嵌入前端整体发布；无旧路由、字段或整数资料源 ID 兼容。历史接口与旧升级方法不适用于当前版本。

## 通用规则

- 家庭账号会话 Cookie：`reelnest_session`，HttpOnly、SameSite=Strict、Path=/，30 天有效；HTTPS 部署启用 Secure。登录轮换当前会话，退出使其失效；进度/收藏由全家共享。
- 除健康、登录和 `/stream/{id}` 外需要会话。写请求校验 Origin / Sec-Fetch-Site；流凭证不能代替会话，Cookie 也不能代替流凭证。
- JSON 请求最多 16 KiB，`Content-Type: application/json`；拒绝未知字段、无效枚举、尾随 JSON、未知/重复查询参数。返回 DTO 不直接序列化数据库行。
- 所有资源 ID 是不透明字符串，时间是 Unix **毫秒**，未发生/不存在用 `null`。进度位置/时长是有限**秒数**。不要把 ID 转为数字或依赖长度/内部结构。
- 读取 JSON 默认 no-store。静态页面路由可回退 SPA，但未知 API/stream/健康路径及缺失静态资源不能回退 HTML。
- 普通错误：`{"error":{"code":"…","message":"…","retryable":false,"requestId":"…"}}`。依赖 `code` 而不是中文文本；删除可能已生效时另用 `DeleteResult`，不能按状态码统一重试。

## 身份、健康、媒体库

| 方法 / 路径 | 请求 / 响应与语义 |
| --- | --- |
| `GET /healthz` | 无鉴权，`{status:"ok"}`，不泄露配置 |
| `GET /readyz` | DB/扫描生命周期就绪状态；不可用 503，不证明上游/NAS 连通 |
| `POST /api/login` | `{username,password}` → 200 `Session` 并 Set-Cookie；失败 401/限流 429 |
| `POST /api/logout` | `{}` → `{ok:true}`；删除当前会话 |
| `GET /api/session` | `{username}`；过期或无登录 401 |
| `GET /api/libraries` | `limit,cursor` → `LibraryPage`（items、allowedRoots、page） |
| `POST /api/libraries` | `{name,path,kind,sources?}` → 201 `Library`；创建后发起首次扫描，返回 latestScanId |
| `PATCH /api/libraries/{id}` | `{revision,name?,sources?}` → `Library`；旧 revision 409 |
| `POST /api/libraries/{id}/scans` | `{}` → 202 `Scan`；同库变更互斥，忙时 409 |
| `GET /api/scans/{id}` | `Scan`：queued/running/completed/failed/interrupted；异步等待终态，不以创建成功当作扫描通过 |

名称 1–80 Unicode 字符；kind=`movie|series`。path 必须是允许根内的绝对目录，不能与应用数据或其他媒体库重叠，不跟随符号链接。sources 是不重复、有序的 `tmdb|tvdb|metatube` 数组，空数组禁用自动获取；MetaTube 仅电影。改名称不改磁盘路径；扫描只在完整成功后发布新索引和清理缺失项。

## 浏览、详情、收藏

| 方法 / 路径 | 响应 |
| --- | --- |
| `GET /api/home` | `Home`：有界 libraries、continueWatching、recent 和 page；跨库最近添加/逐文件续播最多各 20 项 |
| `GET /api/catalog/groups` | `GroupPage`：按完整匹配集合排序的分组摘要；每组 library、total、entriesCursor |
| `GET /api/catalog` | `CatalogPage`：媒体文件卡或电视剧卡的有界 items 和 page |
| `GET /api/media/{id}` | `MediaDetail`：card、文件属性/路径、match；仅查询本目标 |
| `GET /api/series/{id}` | `SeriesDetail`：card、match、有界 seasons 和 page，不内嵌全部单集 |
| `GET /api/series/{id}/seasons/{number}/episodes` | `EpisodePage`：每项一个文件版本、episodeNumber 及 file |
| `PUT /api/favorites/{kind}/{id}` | kind=`media|series`，`{favorite:boolean}` → `Favorite`；要求目标存在，幂等设置，独立于进度 |

### 查询与分页

- `limit` 默认 50、最大 100；首页媒体库入口每页 20、每库预览最多 6。页码不是 offset，不解析 cursor。
- catalog 支持 `libraryId,view,q,sort,limit,cursor`；view=`browse|unmatched|favorites|continue|recent`，sort=`title|recent`。browse/unmatched 使用标题或真实收录时间；favorites/continue/recent 按相应事件时间倒序。
- 无 libraryId 的普通分类/搜索/收藏从 groups 开始，随后对每组使用同样过滤、**同样 limit**、libraryId 和 entriesCursor 取第一页；每组后续页只用自己的 nextCursor。全局 recent/continue 不分组；groups 不接受 recent/continue。
- 子串搜索经 Unicode 规范化，不是全文分词，`%`/`_` 为字面量；单集文件名命中可定位所属电视剧，不改变其完整集数。
- 页尾 `page={nextCursor,expiresAt}`；无后页为 null。签名 keyset cursor 绑定查询/排序/范围/revision，最长 5 分钟，且不超过相关资料的最近到期点。
- 409 `CURSOR_STALE` 后删除该页 cursor 重取，不拼接旧页；数据变化期间不保证跨请求快照。普通列表不因收藏/进度每次变更而失效，但对应收藏/续播页有独立 revision。
- 普通浏览把已组织单集合为电视剧；文件级播放/收藏/续播/删除仍指向文件。卡片无全演职员和全季集数组；需要详情时定向获取。

## 资料、人物、图片、设置

| 方法 / 路径 | 请求 / 响应 |
| --- | --- |
| `GET /api/metadata/subjects/{id}/candidates` | `source,q` → `CandidatePage`：items、complete、subjectRevision、bindingRevision |
| `PUT /api/metadata/subjects/{id}/selection` | `{subjectRevision,expectedRevision,ref}` → `Match` |
| `POST /api/metadata/refresh` | `{libraryId?}` → 202 `RefreshAccepted`；只排队资料任务，不扫描媒体 |
| `GET /api/metadata/status` | `libraryId?,limit,cursor` → `MetadataStatus`，任务计数/revision 和有界失败目标 |
| `GET /api/people/{id}` | `Person`：name、source、photoAssetId 等，不提供跨源名字合并 |
| `GET /api/people/{id}/works` | `limit,cursor` → `PersonWorks`；当前有效资料/本地可用作品，电视剧不按单集重复 |
| `GET /api/metadata/actors/search` | `q` → `ActorSearchPage`：MetaTube 演员姓名/别名查询，最多 50 个去重候选及 truncated 标记 |
| `GET /api/metadata/actors/{id}` | 搜索返回的签名 id → `ActorDetail`：姓名、别名、简介、生日、出道日期、身高等，以及 photoId / wallpapers 图片引用 |
| `GET/HEAD /api/metadata/actor-images/{id}` | 演员查询签发的图片引用 → 图片字节；需要家庭登录，不接受客户端 URL，单图最多 5 MiB |
| `GET/HEAD /api/assets/{id}/content` | 已授权、仍有效的缓存图片字节，不接受任意 URL 代理 |
| `GET /api/settings/metadata` | `Settings`：各来源 URL、revision、catalogId、credentialSet、pinSet、configured；无秘密内容 |
| `PATCH /api/settings/metadata/{source}` | `SettingsPatch` → 更新后的 `ProviderSettings`；错误不回显输入秘密 |

subject 是本地可匹配目标，与 media/series ID 不同，请从详情的 match 取得。ref 必须原样使用候选给出的 source、catalogId、namespace、kind、字符串 remoteId、providerRevision；不是旧整数 sourceId/tmdbId。搜索不写关联，部分/歧义候选不自动确认为唯一匹配。旧候选或配置变化返回 `CANDIDATE_STALE`/`REVISION_CONFLICT`，重新搜索而非覆盖他人选择。

`Match` 分离 selection（automatic/manual）、cacheState（有效/过期/缺失）和后台尝试状态。手动选择保留不意味着过期快照可继续展示。演职员/图片可选部分有独立状态与重试时间；Actor/Director、角色、人物身份均在来源目录空间内处理。图片同 key 合并下载，不同图最多全局 4/同源 2 个；缓存命中不等待其他慢图。

设置必须携带 revision；credential/PIN 省略、null、空白表示保持，clearCredential/clearPin 显式清除。只有 TVDB 接受 PIN，只有 MetaTube 接受 URL。更换 MetaTube 地址时不能沿用旧 Token，须提供新值或清除；新目录空间隔离旧标识。URL 规范化保留反向代理前缀并包含 `/v1`；configured 仅表示具备配置，不是连接测试。重复相同保存不提升 revision，保存不等待资料网络请求。旧环境变量不读取/导入。

### 独立演员查询

页面入口 `/actors`，也可从 MetaTube 本地人物页按当前姓名跳转查询。姓名为 1–256 字符；HTTP 404 搜索结果视为空列表，上游失败与空结果区分。结果不保证覆盖全部上游 Provider，截断/无效条目通过 `truncated` 提示；不自动选择同名人物，不修改影片匹配、人物关系或媒体文件。

演员与图片 id 是服务端 HMAC 签名的短期引用，不是本地 person ID；有效期一小时，绑定 MetaTube 目录空间和配置版本。过期或配置发生变化返回 `CANDIDATE_STALE`，重新搜索即可；签名不能替代家庭会话。查询按需读取 MetaTube 的 lazy 缓存，不新增本地持久化演员/图片缓存或数据库迁移。

头像使用 MetaTube `images/primary`。演员 Provider 不支持 `thumb/backdrop`，壁纸改用 `primary?ratio=0` 保留比例；详情 `images` 中最多 20 个有效、去重 HTTP(S) 引用仅通过已配置 MetaTube 的图片接口代理。客户端不能指定或修改图片 URL，ReelNest 不直接请求这些图片源站、不跟随重定向、不向图片接口发送 Bearer Token。图片缺失时可单独重试，不影响演员资料；图片并发沿用全局 4 / 同源 2 的预算。

## 播放与字幕

| 方法 / 路径 | 请求 / 响应 |
| --- | --- |
| `GET /api/media/{id}/playback` | `Playback`：mediaId、contentPath、progress；先校验当前文件能力 |
| `GET/HEAD /api/media/{id}/content` | 原始文件，支持 Range、If-Range、ETag、条件请求，流式读取已校验句柄 |
| `GET /api/media/{id}/progress` | `{progress: Progress|null}` |
| `PUT /api/media/{id}/progress` | `{position,duration,completed}` → `Progress`；有限秒数，0≤position≤duration，duration>0，最多 366 天 |
| `POST /api/media/{id}/stream-link` | `{}` → `{path:"/stream/{id}?token=…"}`，相对地址，调用方补可达 origin |
| `GET/HEAD /stream/{id}?token=…` | 仅指定文件，无 Cookie 也可读取；不得换 id，凭证无自动过期 |
| `GET /api/media/{id}/subtitles` | `limit,cursor` → `Subtitles`；有界、绑定当前文件版本 |
| `GET /api/media/{id}/subtitles/{subtitle}` | UTF-8 WebVTT，支持由不超过 2 MiB 的有效 SRT 转换，不写源文件 |

流错误中标准 Range/条件响应允许非 JSON（206/304/412/416）。服务端 completed=true 要求位置至少 duration−0.5；网页只在实际 ended 时发送，不靠接近结尾猜测。外部流读取不写进度；流 token 不能读取字幕/图片、改收藏/进度或删除。链接泄露者长期有单文件读取权；没有单链接撤销 API。

## 两步单文件永久删除

1. `POST /api/media/{id}/deletion-preview`，body `{}` → `DeletionPreview`：mediaId、绝对 path、confirmation、expiresAt、warning。确认有效期 10 分钟，不授权其他媒体。
2. 用户核对路径及不可恢复风险后，`POST /api/media/{id}/delete`，body `{confirmation,confirm:true}`。不要自动重试、离线排队或批量调用。
3. HTTP 200 `DeleteResult`：fileOutcome=`deleted|not_deleted|unknown`、indexOutcome=`updated|pending|unchanged`、message、retryable=false、requestId。当前后端 unlink 前失败走普通错误；可能/已经 unlink 才返回结构化结果。

`deleted/pending` 是文件已删除而索引未更新，不是“删除失败可重试”；`unknown` 或传输失败须核查磁盘/重扫，不能断言未执行。只删除视频，字幕/图片/目录不连带。永久删除不可由应用恢复，外部文件系统最终核验与 POSIX unlink 间仍有竞态，NAS 系统调用取消亦非强保证。详见 [安全与回退说明](../README.md)。

## 当前错误分类

| HTTP | code 示例 / 处理 |
| --- | --- |
| 400 | INVALID_REQUEST / INVALID_MEDIA_PATH：修正请求/目录 |
| 401 / 403 | UNAUTHENTICATED / FORBIDDEN：登录或修正同源访问 |
| 404 | NOT_FOUND：资源不存在 |
| 409 | REVISION_CONFLICT、CANDIDATE_STALE、CURSOR_STALE：重新获取当前版本；LIBRARY_BUSY 等待；LIBRARY_OVERLAP、FILE_CHANGED、FILE_ACCESS_DENIED、DELETION_TARGET_CHANGED 须处理具体原因 |
| 410 | FILE_UNAVAILABLE / DELETION_PREVIEW_EXPIRED：缺失/已删或预览过期 |
| 422 / 429 | SUBTITLE_INVALID / RATE_LIMITED；限流有 Retry-After |
| 503 | PROVIDER_UNAVAILABLE、METADATA_UNAVAILABLE、ASSET_UNAVAILABLE、NOT_READY：不证明媒体本身不可读 |
| 500 | DATABASE_UNAVAILABLE / 内部故障；删除请求仍不得盲目重试 |

契约检查覆盖真实响应；文件安全、来源网络、SQL 事务与身份授权由各自 module 测试保证，不把 OpenAPI 校验等同于业务授权。完整测试与未验证环境见 [最终交付记录](architecture/final-delivery.md)。
