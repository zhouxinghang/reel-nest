# 旧版应用 HTTP 契约（历史记录，待 06 重写）

> 2026-10-02：正式源码已经切换到新契约。**下表不再是当前 API，也不提供旧路由兜底。** 当前字段、方法与错误结构以 [`api/openapi.yaml`](../api/openapi.yaml) 为准，集成边界见[05 交接](architecture/frontend-interactions.md)。

应用与网页同源。所有写入请求为 `application/json`，无参数 POST 发送 `{}`；错误返回 `{"error":"可显示的错误说明"}`。对跨站浏览器写入请求返回 403。除健康检查和登录外，API 均要求家庭共享账号会话；独立的 `/stream/{id}` 使用单文件只读串流凭证，不能代替网页会话。

| 请求 | 成功响应 / 行为 |
| --- | --- |
| `GET /healthz` | `200 {"status":"ok"}`，不泄露配置 |
| `POST /api/login` | 输入 `username/password`，200 返回 `username`，同时设置会话 Cookie；错误凭据 401，连续失败过多 429 |
| `GET /api/session` | 200 返回家庭共享账号的 `username`，不返回密码或哈希 |
| `POST /api/logout` | 200 `{"ok":true}`，立即撤销当前会话；不撤销其他浏览器 |
| `GET /api/settings/metadata` | 200 `{sources:[{id,name,url,credentialSet,pinSet,configured,revision}]}`；仅返回凭据存在状态，绝不返回凭据/掩码；`configured` 不代表联网验证 |
| `POST /api/settings/metadata/{source}` | source 为 `tmdb/tvdb/metatube`；输入 `{revision,credential?,clearCredential?,pin?,clearPin?,url?}`，200 返回更新后的单来源公开配置；无效 400，未知来源 404，版本冲突或匹配忙 409，保存失败 500 |
| `GET /api/directories` | 200 `{directories, allowedRoots, metadataSources}`；只暴露已配置允许根目录和已接入目录，不提供服务器目录树枚举 |
| `POST /api/directories` | 输入 `{name, path, kind, metadataSources?}`；name 去除首尾空白后须为 1–80 个 Unicode 字符且无控制字符，否则 400，kind 为 `movie` 或 `series`；201 返回已接入目录并启动首次扫描；越界/符号链接/非目录 400，重复或重叠目录 409 |
| `POST /api/directories/{id}/name` | 输入 `{name}`，200 返回规范化的 `{name}`；名称规则与创建相同，无效 400，不存在 404；只修改展示名称，不重扫、不改源目录 |
| `POST /api/directories/{id}/metadata-sources` | 输入 `{metadataSources:["tvdb","tmdb"]}`，200 保存启用来源与优先级；空数组禁用自动获取，校验失败 400，资料写入争用 409 |
| `POST /api/directories/{id}/scan` | 202 `{"scanStatus":"running"}`，异步重新扫描；已在扫描时 409，不存在 404 |
| `GET /api/library` | 200 `{items:[媒体文件...], series:[电视剧摘要...], directories:[具名目录...]}`；支持 `directoryId` 限定单个媒体库（未指定为全部，不存在 404）， `q` 文件名/本地电视剧名/资料标题子串搜索、`kind=movie/series` 分类、`sort=recent` 按实际收录时间降序；未指定排序沿用文件名排序，未知分类/排序 400 |
| `POST /api/favorites/{kind}/{id}` | 家庭共享账号添加/取消收藏；kind 为 `media`（电影或单集文件）或 `series`（整部电视剧），输入 `{"favorite":true/false}`，200 返回 `{favorite,favoritedAt}`；非法类型/缺失或无效字段 400，对象不存在 404 |
| `GET /api/media/{id}` | 200 返回单个媒体文件详情，包含可空的季集关联；不存在 404；标识不是服务器文件路径 |
| `GET /api/series/{id}` | 200 返回电视剧摘要及按季号/集号排序的 `seasons` 层级；不存在 404；媒体文件标识不能充当电视剧标识 |

目录字段：`id / name / path / kind / scanStatus / scanError / scanStarted / scanFinished / metadataSources`。状态为 `pending / running / completed / failed / interrupted`。时间为 Unix 秒，未开始/完成为 0。新增或重扫响应仅代表请求已接受；应轮询目录列表观察终态，不按固定延时假定成功。

一个已接入目录对应一个具名媒体库；名称不是身份，同名媒体库仍按目录 ID 隔离。旧目录启动迁移时用文件夹名补全名称（去除控制字符并截取至 80 字符，空名回退“媒体库”），无需读取媒体文件或重扫。

网页 `/library?directory={id}` 为单库入口，刷新/浏览器前进后退保持选择；全局分类、搜索、收藏按目录分组展示；首页「最近添加」跨目录统一展示最新 20 项，完整入口 `/library?type=recent` 也不按目录分组，按实际收录时间统一排序。电视剧仍保留季集组织。`/api/library` 的 `directories` 始终包含全部已接入目录（含空库），只有 `items/series` 应用查询条件。

媒体文件字段：`id / directoryId / directoryName / directoryPath / filename / relativePath / path / kind / size / modifiedAt / available / matched / episode / organizationReason / progress / match`。大小为字节，修改时间为 Unix 秒；`available` 是成功删除后的即时索引状态，不是实时探测结果。完整成功扫描会移除未发现的文件记录；扫描失败或中断不发布部分结果。`matched` 表示存在未过期的影片资料。

`episode` 为 `null`（电影或未组织文件），或 `{seriesId, seriesTitle, seasonNumber, episodeNumber}`。无法识别的电视剧文件通过 `organizationReason` 解释原因；识别成功时为空字符串。`matched` 表示影片资料匹配，不能用 `episode != null` 代替它。

电视剧摘要字段：`id / title / directoryId / directoryName / directoryPath / seasonCount / episodeCount / fileCount / availableFileCount / matched / match`。季/集数量按逻辑季集去重，文件数量不合并版本；来源目录区分跨目录的同名作品。`/api/library` 的摘要对应本次筛选出的文件；`items` 始终保留每个文件，不被电视剧摘要替代。

媒体文件与电视剧摘要还包含 `addedAt / favorite / favoritedAt`。`addedAt` 为首次完整成功扫描收录该文件的 Unix 毫秒；整部电视剧取当前所有单集文件的最大值，新单集入库会更新电视剧的最近添加排序。旧版未记录历史日期，升级时保留为 0（未知），不使用文件修改时间或升级时间伪造；普通重扫不改写。`sort=recent` 以时间降序、相同时间按稳定标识排序，历史未知项在后。

`favorite` 为家庭共享收藏状态；`favoritedAt` 为首次设置本次收藏的 Unix 毫秒，未收藏为 0。重复设置 true 保留原时间，设置 false 删除收藏，重复 false 仍返回 false/0。文件与整部电视剧独立收藏，不改写观看进度、资料或源文件；数据库持久化，目标记录删除时级联清理。收藏写入不接受额外 JSON 字段，须使用会话且通过同源检查；只读串流凭证不能授权。网页「收藏」按此状态筛选，按 `favoritedAt` 降序展示。

`GET /api/series/{id}` 在摘要之外返回：

```json
{
  "seasons": [
    { "number": 1, "episodes": [
      { "number": 2, "files": ["完整媒体文件对象，每个版本独立"] }
    ] }
  ]
}
```

同一个媒体文件至多关联一个季集位置，不以作品标题或季集编号作为读取/修改目标。重复扫描和版本升级保留仍存在文件的标识；同一单集下可有多个独立版本。完整成功扫描会移除已不存在的版本。

未登录 API 返回 401；受保护页面 `/library`、`/directories`、`/media/{id}`、`/series/{id}` 引导至 `/login`。电视剧页面使用 `?season=2` 选择第 2 季；不存在的季显示明确提示。前端资源不包含秘密。删除必须使用下面的独立两步接口，文件详情不授予删除权限；不能将详情响应当成播放验证。

扫描在真实文件系统根句柄内进行，拒绝符号链接并检测根目录替换；失败扫描不发布部分索引。应用数据（SQLite，WAL 模式）独立保存；按目录和文件版本建立稳定随机文件标识。身份识别的精确边界见 README。


## 原始视频与共享观看进度（04）

| 请求 | 成功响应 / 行为 |
| --- | --- |
| `GET /api/media/{id}/playback` | 重新核对文件/目录身份并打开真实文件；200 返回当前进度，不写进度；播放器预检及错误诊断 |
| `GET/HEAD /api/media/{id}/content` | 会话鉴权后按需读取原始文件。200 全文件，206 有效范围（包括多段），HEAD 无文件体；ETag 绑定文件标识，支持 If-Range |
| `GET /api/media/{id}/progress` | 200 `{position,duration,completed,updatedAt}`；无已保存进度时全部为 0/false。删除后、下次成功扫描前仍可读历史进度 |
| `POST /api/media/{id}/progress` | 输入 `{position,duration,completed}`；200 返回保存值。按文件身份覆盖家庭共用进度，不区分浏览器 |

媒体/字幕读取的 404 表示未知媒体、已被成功扫描移除的媒体或未知字幕；410 表示索引仍存在但源文件已经缺失或被删除；409 表示文件/目录变化、挂载或权限错误；不会沿旧路径读取另一个版本。读取时逐级检查根句柄、拒绝符号链接，再以同一真实文件句柄返回字节。文件身份沿用元数据指纹约定。HTTP Range 无效返回 416（由 HTTP 文件服务产生的错误可以是纯文本而非 JSON）；不以成功响应伪装不满足的范围。

进度 `position`、`duration` 单位为秒，可为小数；`updatedAt` 为服务端 Unix 毫秒。写入必须包含全部三个非 null 字段；要求有限数值、`0 <= position <= duration`、`0 < duration <= 366 天`。`completed=true` 只接受结束点附近（最后 0.5 秒）；网页只在原生 ended 事件后提交完成，不按百分比推断。无效输入 400、未知媒体 404、未授权 401。保存不修改源媒体或文件身份；未知/多余 JSON 字段拒绝。

媒体库、详情和季集文件对象增加 `progress`：未保存时为 `null`，否则是上述进度对象。「继续观看」由可用文件中 position > 0 且未完成的记录组成，按更新时间排序。

## 单文件只读串流（05）

| 请求 | 成功响应 / 行为 |
| --- | --- |
| `POST /api/media/{id}/stream-link` | 需要网页会话，输入 `{}`；核对实际文件后，200 `{path:"/stream/{id}?token=…"}`。网页以自己的 origin 构造可复制地址 |
| `GET/HEAD /stream/{id}?token=…` | 不需要 Cookie；合法的单文件凭证使用与网页相同的按需读取/范围契约。缺失、重复、畸形、篡改或绑定另一个文件的 token 返回 401 |

凭证为使用持久化 32 字节随机密钥对域分隔前缀 + 媒体文件标识的 HMAC-SHA256（64 字符十六进制）。不包含过期时间、不受网页退出影响，不根据客户端 Host/Forwarded 头生成地址；没有链接撤销/续签接口。整个数据库及 WAL 应按部署文档备份。错误响应不回显 token，常规应用日志不输出请求 URL；反向代理也应禁记查询字符串。

只读路由仅支持 GET/HEAD，管理 API 始终只认家庭共享账号 Cookie：把 token 搬到 `/api/*` 的查询参数不会获得权限。串流不提供字幕、浏览、扫描或进度写入，也不产生任何观看记录。源文件变化或尚未重扫的缺失返回实际 409/410，完整扫描移除记录后返回 404；不接受 Cookie 作为串流路由的授权兜底。

## 外挂字幕（06）

| 请求 | 成功响应 / 行为 |
| --- | --- |
| `GET /api/media/{id}/subtitles` | 需要会话及可用原视频；200 `{items:[{id,filename,format}]}`，无匹配项时 items 为空数组 |
| `GET /api/media/{id}/subtitles/{subtitle}` | 需要会话；校验该视频的同目录候选与当前文件指纹；200 `text/vtt; charset=utf-8`，SRT 会实际转换为 VTT |

字幕 id 是媒体标识、候选文件名和文件指纹的 SHA-256，不是文件路径，也不是授权凭证；只认当前候选集合，不能指定任意路径。读取拒绝符号链接、非常规文件和读取过程中发生的替换。候选变化返回 404，权限/读取竞态 409；格式、编码、结构、时间错误或超过 2 MiB 返回 422 JSON 错误。读取/转换不修改源字幕；详细命名/格式子集见 README。

网页预先获取并验证 VTT，再通过本地 Blob 交给原生 `<track>` 渲染，选择切换会禁用/移除旧 track、取消未完成请求并释放 Blob。CSP 仅为原生媒体增加 `blob:` 来源，脚本仍只允许同源。字幕错误不会停止视频或伪造加载成功。

## 页面资料源配置（19）

- `/settings/metadata` 需要家庭共享账号会话，可从管理菜单、媒体目录和资料状态栏进入；来源服务设置全家共用，每目录启用/优先级仍使用原接口。
- 写入仍受统一的 8 KiB JSON 请求体上限约束；较长的凭据和 PIN 可以分别保存，省略字段不会清空已有值。
- `revision` 必填，为刚读取的该来源版本（正整数）。成功变更只递增该来源版本；旧页面写入返回 409，禁止覆盖较新设置。无变化保存不改版本、不重建客户端或重置冷却。
- `credential` 对 TMDB 是 Read Access Token，对 TVDB 是 API Key，对 MetaTube 是 Token。凭据/PIN 去首尾空白，最多 4096 字节且不能含内部空白或控制字符。省略、null 或空字符串均保留；`clearCredential:true` / `clearPin:true` 显式清除，不能同时传非空替换值。`pin/clearPin` 仅 TVDB 可用。
- `url` 仅 MetaTube 可用，省略/null 保持，空字符串禁用服务；最多 2048 字节，必须是无 userinfo/query/fragment 的 HTTP/HTTPS 地址，允许反向代理路径和 `/v1` 后缀。若已有 Token，更改地址必须同时提供替换 Token 或 `clearCredential:true`，不能静默把旧凭据带往新服务。
- 仅成功保存后替换来源客户端；登录 token 和冷却状态不跨新配置复用。与关联写入互斥，获取资料期间返回 409。已经在途的搜索/图片请求可用旧客户端结束；保存后新请求使用新客户端。失败不改变运行客户端和保存值；不重扫、不改媒体身份/进度/收藏，不覆盖手动匹配。
- 凭据/PIN 以 AES-256-GCM 密文保存在 `metadata_provider_settings`，绑定来源及服务地址作为附加认证数据；随机 32 字节密钥单独位于数据目录 `metadata-settings.key`。SQLite、密钥均为 0600；恢复需要完整数据目录，已有设置却无密钥时拒绝启动，不生成新密钥覆盖。完整备份须视为敏感数据。
- 首次无保存配置时导入旧环境凭据（文件优先），之后不再读取或应用旧变量/文件；页面清除也不会被环境恢复。新部署在页面填写，不需要资料源环境变量或 Compose override。公共 TMDB/TVDB API 地址不对网页开放。
- GET、POST、错误和日志不回显凭据；同源 JSON 写入和会话鉴权沿用全局规则，串流只读凭证不能授权，响应均禁止缓存。只接受当前来源的字段，不接受前端传来的任意服务端配置。

## 目录资料源（12）

- 目录字段 `metadataSources` 为不重复的有序数组，可含 `tmdb`、`tvdb`、`metatube`；`[]` 暂停自动获取。创建时省略/传 null 均采用默认 `["tmdb","tvdb"]`；修改接口必须传数组，null、未知来源、重复来源和超过三项均 400。每个目录的递归扫描内容共享此配置。
- `GET /api/directories` 的顶层 `metadataSources` 是支持来源的状态列表 `[{id,name,configured,cooldownUntil}]`，不包含凭据；与各目录的有序 ID 数组区分。
- `POST /api/directories/{id}/metadata-sources`，`{"metadataSources":["tvdb","tmdb"]}` → 200 返回 `{metadataSources}`，排队按新顺序评估自动匹配。不重扫/改写媒体文件、不清除进度、不覆盖手动选择；不存在 404，资料写入争用 409，未登录 401。
- 首个来源不可用或无唯一可靠结果时继续下一源，成功则停止；海报/简介/演职员不完整不跨来源拼接。优先级不变且缓存有效时不重复请求基本资料，缺失/待重试演职员按下述规则补齐。保存新顺序后获取失败，保留原有效资料并报告原因；禁用来源不删除未到期缓存。

## TMDB / TVDB / MetaTube 影片资料（07、12、17）

所有以下路由都要求家庭共享账号；串流凭证不授予权限。

- `GET /api/metadata/status` → `{configured,sources,running,error,finishedAt,revision,cooldownUntil}`。`configured` 为至少一个来源已配置（MetaTube 以服务地址为准）；`sources` 为 `[{id,name,configured,cooldownUntil}]`。凭据本身从不返回，已配置不等同于已验证可连接。`revision` 在后台批次完成、手动确认成功或清理到期缓存后递增；`finishedAt` / `cooldownUntil` 为 Unix 秒。
- `POST /api/metadata/refresh`，空 JSON `{}` → 202 排队；未配置 503、已有批次 409。不扫描磁盘；对已收录电影和已组织电视剧尝试唯一精确匹配，刷新到期关联，也补齐缺失或已到重试时间的演职员。有效标题/海报沿用；已有手动关联的未组织电视剧文件也刷新已选资料。每个来源至多 20 秒预算，整项操作至多 65 秒，各来源独立冷却。
- `GET|HEAD /api/metadata/posters/{source}/{kind}/{sourceId}`（source 为 `tmdb|tvdb|metatube`；保留旧 `/api/metadata/posters/{kind}/{tmdbId}` 为 TMDB 别名） → 已缓存有效图片；未配置不影响未到期缓存读取。无海报、错误类型/标识、到期返回 404；未登录 401。响应 `no-store`，避免浏览器长期保留在线图片。无任意 URL/路径代理。
- 媒体文件与电视剧响应新增 `match: {metadata,selection,status,error}`。`metadata` 为 `null` 或 `{source,sourceId,tmdbId,kind,title,originalTitle,year,overview,posterUrl,sourceUrl,fetchedAt,expiresAt}`；`source` 为 `TMDB|TVDB|MetaTube`，`sourceId` 是来源内正整数 ID。兼容字段 `tmdbId` 仅 TMDB 时有值，TVDB / MetaTube 为 0；缓存键含来源，不能以数字 ID 跨源合并。`kind` 为 `movie|series`，`selection` 为 `automatic|manual` 或尚未关联时空字符串，`status` 为 `unmatched|matched|failed|expired`。`matched` 表示当前有未到期的可展示资料，而不是视频是否可播放。资料失效不改变本地媒体 ID、季集关系或观看进度。
- `GET /api/library?q=` 同时搜索本地文件名、本地电视剧名和有效资料标题；既有字段保留，电视剧 `title` 仍是本地组织名称，中文资料通过 `match.metadata.title` 提供。

### 演职员字段（13）

`TitleMetadata` 增加可选 `credits`；旧缓存及搜索候选可以没有此字段（待补齐，不应当作空名单）：

```json
{
  "credits": {
    "status": "available",
    "version": 1,
    "cast": [{"id": 101, "key": "101", "name": "演员姓名", "photoUrl": "/api/people/tmdb/101/photo", "roles": ["角色甲", "角色乙"]}],
    "directors": [{"id": 201, "key": "201", "name": "导演姓名"}],
    "fetchedAt": 1790800000
  }
}
```

- `status=available` 表示成功（包含空 `cast:[] / directors:[]`），`failed` 表示演职员请求失败，附 `error` 和 Unix 秒 `retryAt`；`fetchedAt` 为名单获取/首次失败时间；保留旧名单的升级失败不改写其原获取时间。首次获取失败时两名单为空，但 `match.status` 仍可为 `matched`，标题和海报仍有效。旧成功演职员的照片引用升级失败则保留名单及 `status=available`，附 `error/retryAt`，不清空原有资料。新匹配及手动确认的演职员失败不使整次标题关联失败。
- 人物 `id` 只在 `metadata.source` 内有效；来源缺 ID 时为 0。`name` 为真实人物姓名；`roles` 为空表示未提供角色，不使用角色名代替演员。上限 100 位演员 / 50 位导演，超限附 `truncated:true`。不含凭据或跨源人物身份。人物 `key` 用于站内导航；ID 缺失时为当前作品作用域的 `local-<hash>`，不可跨作品按姓名合并。可选 `photoUrl` 是同源人物照片接口；可选 `photoPath` 是适配器校验后的缓存图片引用，前端不直接使用。
- 电影/电视剧详情、媒体文件、媒体库均使用同一 `TitleMetadata`。已组织单集的演职员是其**整部电视剧**汇总资料，不提供单集专属推断。
- 旧有效 payload 不含 `credits` 或 `credits.version<1` 时后台补齐；失败的补齐请求至少间隔 24 小时，由后续匹配批次重试，状态跨重启保留。只请求当前作品同一已启用/已配置来源，不重新搜索、不更改关联/策略/TTL/海报/进度。成功空名单不视为待重试。

## 人物与本地关联作品（14）

- `GET /api/people/{source}/{key}` → `{id,key,name,source,photoUrl?,photoPath?,works:[]}`。`source` 路由为小写 `tmdb|tvdb|metatube`，响应为 `TMDB|TVDB|METATUBE`；`key` 从演职员字段取得。未知来源/格式 400，无当前有效关联 404，未登录 401。仅查询 SQLite 的当前匹配关系，不查询外部人物作品年表。
- `works` 元素为 `{id,targetKind,available,metadata,jobs,characters}`；`targetKind=media|series` 用于导航到 `/media/{id}` 或 `/series/{id}`，`metadata.kind=movie|series` 用于分类。`metadata` 为影片资料但省略演职员；`jobs` 为 `演员/导演` 字符串数组，`characters` 为饰演角色。按年份降序、标题排序。同源同类同作品 ID 合并，优先可用目标；电视剧不展开单集。缺人物 ID 仅返回该作品范围，同名/跨源不自动合并。
- `GET|HEAD /api/people/{source}/{key}/photo` → 缓存图片或按需请求其当前有效关联中的图片引用。接口不接受任意 URL/路径，来源适配器重新校验图片 origin/path；不跟随重定向，不转发凭据。只支持 JPEG/PNG/WebP，至多 5 MiB，并校验声明 MIME 与内容签名一致。未登录 401，无引用、没有有效关联或图片获取失败 404，缓存故障 500，等待图片队列超时 503。
- 照片成功缓存最长 30 天，失败重试间隔 24 小时，均存入 `person_photos`；同一来源/图片引用共享缓存。HTTP 为 `no-store`、`nosniff`；未到期的 SQLite 图片在重启或撤下凭据后仍可读取，但必须存在当前有效关联。图片请求独立节流/冷却，不令标题匹配进入冷却；标题和播放 API 不等待图片下载。过期缓存定期清理，重访触发刷新，不定时下载照片。
- 页面 `/people/{source}/{key}` 要求共享账号；演员/导演卡片均可进入，支持电影/电视剧筛选、作品详情跳转、缺图占位、无关联/加载失败提示和重新加载。

## 手动修正影片资料（08）

- `GET /api/matching/{kind}/{id}/search?q=<作品标题>` → `{items:[TitleMetadata...],moreResults,warnings}`。`kind=movie|series`；查询去首尾空白后 1–200 UTF-8 字节，参数错误 400，目标不存在/类型不符 404，未登录 401，全部已启用来源不可用（或未配置）503。按目录优先级返回每源至多 20 个同类型候选；部分失败仍返回成功来源的候选并在 warnings 中提示，包含标题/原始标题/年份/来源/ID；搜索从不修改关联。
- `POST /api/matching/{kind}/{id}/selection`，`{kind,source,sourceId}` → 200 返回新 `match`。`source` 接受大小写的 tmdb/tvdb/metatube，必须在目录启用，`sourceId` 为正整数；保留 `{kind,tmdbId}` 作为旧 TMDB 请求格式，body 类型必须与路由相同；类型/ID 不符 400，目标不存在 404，后台匹配或其他确认争用时 409，外部失败 503，持久化失败 500。确认成功后 `selection=manual`；失败保留旧关联，取消无需请求。
- `movie` 的 `id` 为该电影媒体文件 ID。`series` 的 `id` 为本地电视剧 ID；对尚未组织到电视剧的文件，也接受该电视剧文件 ID，仅关联资料而不创建季集结构。已组织单集须使用其 `episode.seriesId`，不能把某一版本确认伪装成整个电视剧之外的新内容。
- 稳定文件 ID、源文件、其他电影版本、季集关系和观看进度不受影响。过期缓存刷新沿用手动选择的来源、类型和 ID（来源仍启用时）；正常重扫/重启不回退为自动匹配。

## 单文件永久删除（09）

两条路由均要求家庭共享账号 Cookie，并应用同源/JSON 校验；串流凭证及确认字符串本身不构成管理授权。无路径、目录、季集或批量删除接口。

| 请求 | 输入 / 结果 |
| --- | --- |
| `POST /api/media/{id}/deletion-preview` | 输入 `{}`；仅校验文件，不修改文件或索引。200 `{path,confirmation,expiresAt}`；path 是实际应用可见绝对路径，expiresAt 为 Unix 秒，确认有效期 10 分钟 |
| `POST /api/media/{id}/delete` | 输入 `{confirmation,confirmed:true}`；重新验证目标后才尝试永久删除该单个视频。200 `{deleted:true,indexUpdated:boolean,message}` |

`confirmation` 使用独立域分隔 HMAC，绑定有效期、媒体标识、完整路径、父目录身份与文件设备/inode/模式/大小/mtime/ctime；它不是只读串流凭证。过期/缺失/错误确认、目标变化或不可确认返回 409；未知媒体 404，缺失或已标记不可用 410；删除系统调用的权限不足/只读挂载 403。参数外的路径或数组拒绝为 400。拒绝请求不提前清除索引，不将不存在文件无条件当作成功。

200 **不表示两个步骤都成功**：必须检查 `indexUpdated`。false 意味着**文件确已永久删除**但 SQLite 更新失败，message 明确要求修复存储并重扫，不能自动重试删除或表示可恢复。文件删除后即使 HTTP 客户端取消，服务端仍以独立有界上下文尝试更新索引。网络/进程/远端存储异常可能导致调用方不知道实际结果，须核实文件并扫描；这不是跨文件系统和数据库的原子事务。

正常成功后先保留文件记录和已有资料/季集/进度，仅置 `available=false`；后续内容、字幕、播放检查和旧串流请求返回 410。下一次完整成功扫描会移除该记录及对应进度/文件级资料关联，旧接口与串流链接随后返回 404。文件不存在或已替换时读取也会独立拒绝，即使索引写入曾失败。已有打开的媒体句柄可能继续读取。扫描与删除在本进程串行化；外部宿主机/NAS 写入竞态的边界见 README，不承诺 POSIX 原子按 inode 条件删除。


### 多类型影片图片（Primary / Thumb / Backdrop）

`TitleMetadata` 在兼容 `posterUrl` 的基础上增加 `thumbUrl`、`backdropUrl`（没有该类图片时为空字符串）。`artwork` 记录补充获取状态：`version`、`status: available|failed`、`fetchedAt`、可选 `retryAt` / `error`；旧缓存可能没有此对象，后台升级后补齐。`available` 允许没有图片，不代表错误。

- `GET|HEAD /api/metadata/images/{source}/{kind}/{id}/{type}`：`source` 为 `tmdb|tvdb|metatube`，`kind` 为 `movie|series`，`type` 为 `thumb|backdrop`。须家庭账号会话，串流凭证不能授权。返回当前有效资料所选图片，`Content-Type` 为 JPEG/PNG/WebP，HEAD 仅返回响应头；路径不接收外部 URL。
- `posterUrl` 继续使用原 `/api/metadata/posters/...` 接口，无需迁移旧客户端。
- 首次横图请求按需获取并持久缓存，5 MiB 上限、签名校验、不跟随重定向；图片服务无 API Authorization。后续请求读取缓存，未命中并发合并；失败 24 小时后才再次尝试。已有引用可在资料 API 凭据撤下后读取。
- `401`：未登录；`404`：非法来源/类型/ID、缺图、资料到期或图片失败退避；`503`：请求等待取消/超时；`500`：存储错误。不会回退返回错误类型图片，由前端按场景选择下一种图片。原缓存到期后不继续提供图片。
- 电影和整部电视剧分别持有图片，已组织单集继承电视剧图片。优先级由其当前资料来源决定，不跨来源拼接。旧缓存补齐不重写关联、不延长 TTL、不改变原海报与演职员。

## MetaTube 适配约定（17）

- 通过 `/settings/metadata` 页面及受会话保护的配置 API 保存服务地址和 Token；地址可回显，Token 只返回是否已保存。旧环境变量仅首次迁入，后续不再读取；详见页面资料源配置（19）。
- 目录默认仍为 `["tmdb","tvdb"]`；可显式设置 `["metatube","tmdb","tvdb"]`。MetaTube 仅接受 `movie`，电视剧不发起上游请求并提示不支持，不伪造季集。
- `metadata.source="MetaTube"`；可选 `number` 为影片编号、`provider` 为 MetaTube 上游 Provider。`sourceId` 是本地持久映射的正整数，不是上游字符串 ID。客户端只使用搜索返回的 `sourceId` 确认；不能自己转换/拼接 ProviderID。Provider 名忽略大小写，原始 ID 区分大小写。
- 搜索关键词原样以 `q` 转发（含 URL），`fallback=true`；空数组、省略 data 或搜索 404 返回空候选。影片详情使用 `lazy=true`，核对返回 provider 与 id；失败不改变原选择。完整详情自带演职员/图片引用时不重复请求。
- 标题或 `number` 在保守规范化后唯一精确匹配才自动确认，仍要求年份匹配；跨 Provider 同编号的多个候选不自动选中。最多展示 20 项，超限或丢弃无效项时 `moreResults=true`，不进行自动匹配。
- 影片图片复用现有本地认证缓存路由；保存的是受控标识引用，不接受任意 URL。影片只给演员/导演姓名，人物 ID 为 0、key 为作品作用域的 local 标识，不臆造照片或跨作品关系。
