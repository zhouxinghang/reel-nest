# 当前应用 HTTP 契约（01–06）

应用与网页同源。所有写入请求为 `application/json`，无参数 POST 发送 `{}`；错误返回 `{"error":"可显示的错误说明"}`。对跨站浏览器写入请求返回 403。除健康检查和登录外，API 均要求家庭共享账号会话；独立的 `/stream/{id}` 使用单文件只读串流凭证，不能代替网页会话。

| 请求 | 成功响应 / 行为 |
| --- | --- |
| `GET /healthz` | `200 {"status":"ok"}`，不泄露配置 |
| `POST /api/login` | 输入 `username/password`，200 返回 `username`，同时设置会话 Cookie；错误凭据 401，连续失败过多 429 |
| `GET /api/session` | 200 返回家庭共享账号的 `username`，不返回密码或哈希 |
| `POST /api/logout` | 200 `{"ok":true}`，立即撤销当前会话；不撤销其他浏览器 |
| `GET /api/directories` | 200 `{directories, allowedRoots}`；只暴露已配置允许根目录和已接入目录，不提供服务器目录树枚举 |
| `POST /api/directories` | 输入 `{path, kind}`，kind 为 `movie` 或 `series`；201 返回已接入目录并启动首次扫描；越界/符号链接/非目录 400，重复或重叠目录 409 |
| `POST /api/directories/{id}/scan` | 202 `{"scanStatus":"running"}`，异步重新扫描；已在扫描时 409，不存在 404 |
| `GET /api/library` | 200 `{items:[媒体文件...], series:[电视剧摘要...]}`；支持 `q` 文件名/本地电视剧名子串搜索、`kind=movie/series` 分类；未知分类 400 |
| `GET /api/media/{id}` | 200 返回单个媒体文件详情，包含可空的季集关联；不存在 404；标识不是服务器文件路径 |
| `GET /api/series/{id}` | 200 返回电视剧摘要及按季号/集号排序的 `seasons` 层级；不存在 404；媒体文件标识不能充当电视剧标识 |

目录字段：`id / path / kind / scanStatus / scanError / scanStarted / scanFinished`。状态为 `pending / running / completed / failed / interrupted`。时间为 Unix 秒，未开始/完成为 0。新增或重扫响应仅代表请求已接受；应轮询目录列表观察终态，不按固定延时假定成功。

媒体文件字段：`id / directoryId / directoryPath / filename / relativePath / path / kind / size / modifiedAt / available / matched / episode / organizationReason / progress`。大小为字节，修改时间为 Unix 秒；`available` 是最近一次完整扫描的文件版本状态，不是实时探测结果。当前尚未接入影片资料，`matched=false`；即使不可用仍可查看记录和源文件路径。

`episode` 为 `null`（电影或未组织文件），或 `{seriesId, seriesTitle, seasonNumber, episodeNumber}`。无法识别的电视剧文件通过 `organizationReason` 解释原因；识别成功时为空字符串。`matched` 表示影片资料匹配，不能用 `episode != null` 代替它。

电视剧摘要字段：`id / title / directoryId / directoryPath / seasonCount / episodeCount / fileCount / availableFileCount / matched`。季/集数量按逻辑季集去重，文件数量不合并版本；来源目录区分跨目录的同名作品。`/api/library` 的摘要对应本次筛选出的文件；`items` 始终保留每个文件，不被电视剧摘要替代。

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

同一个媒体文件至多关联一个季集位置，不以作品标题或季集编号作为读取/修改目标。重复扫描和版本升级保留原文件标识；同一单集下可有多个独立版本，包括已不可用的旧文件记录。

未登录 API 返回 401；受保护页面 `/library`、`/directories`、`/media/{id}`、`/series/{id}` 引导至 `/login`。电视剧页面使用 `?season=2` 选择第 2 季；不存在的季显示明确提示。前端资源不包含秘密。TMDB 匹配或删除尚未实现，不应将文件详情响应当成播放验证。

扫描在真实文件系统根句柄内进行，拒绝符号链接并检测根目录替换；失败扫描不发布部分索引。应用数据（SQLite，WAL 模式）独立保存；按目录和文件版本建立稳定随机文件标识。身份识别的精确边界见 README。


## 原始视频与共享观看进度（04）

| 请求 | 成功响应 / 行为 |
| --- | --- |
| `GET /api/media/{id}/playback` | 重新核对文件/目录身份并打开真实文件；200 返回当前进度，不写进度；播放器预检及错误诊断 |
| `GET/HEAD /api/media/{id}/content` | 会话鉴权后按需读取原始文件。200 全文件，206 有效范围（包括多段），HEAD 无文件体；ETag 绑定文件标识，支持 If-Range |
| `GET /api/media/{id}/progress` | 200 `{position,duration,completed,updatedAt}`；无已保存进度时全部为 0/false。文件不可用时仍可读历史进度 |
| `POST /api/media/{id}/progress` | 输入 `{position,duration,completed}`；200 返回保存值。按文件身份覆盖家庭共用进度，不区分浏览器 |

媒体/字幕读取的 404 表示未知媒体或未知字幕；410 表示扫描已确认不可用或原文件已缺失；409 表示文件/目录变化、挂载或权限错误；不会沿旧路径读取另一个版本。读取时逐级检查根句柄、拒绝符号链接，再以同一真实文件句柄返回字节。文件身份沿用元数据指纹约定。HTTP Range 无效返回 416（由 HTTP 文件服务产生的错误可以是纯文本而非 JSON）；不以成功响应伪装不满足的范围。

进度 `position`、`duration` 单位为秒，可为小数；`updatedAt` 为服务端 Unix 毫秒。写入必须包含全部三个非 null 字段；要求有限数值、`0 <= position <= duration`、`0 < duration <= 366 天`。`completed=true` 只接受结束点附近（最后 0.5 秒）；网页只在原生 ended 事件后提交完成，不按百分比推断。无效输入 400、未知媒体 404、未授权 401。保存不修改源媒体或文件身份；未知/多余 JSON 字段拒绝。

媒体库、详情和季集文件对象增加 `progress`：未保存时为 `null`，否则是上述进度对象。「继续观看」由可用文件中 position > 0 且未完成的记录组成，按更新时间排序。

## 单文件只读串流（05）

| 请求 | 成功响应 / 行为 |
| --- | --- |
| `POST /api/media/{id}/stream-link` | 需要网页会话，输入 `{}`；核对实际文件后，200 `{path:"/stream/{id}?token=…"}`。网页以自己的 origin 构造可复制地址 |
| `GET/HEAD /stream/{id}?token=…` | 不需要 Cookie；合法的单文件凭证使用与网页相同的按需读取/范围契约。缺失、重复、畸形、篡改或绑定另一个文件的 token 返回 401 |

凭证为使用持久化 32 字节随机密钥对域分隔前缀 + 媒体文件标识的 HMAC-SHA256（64 字符十六进制）。不包含过期时间、不受网页退出影响，不根据客户端 Host/Forwarded 头生成地址；没有链接撤销/续签接口。整个数据库及 WAL 应按部署文档备份。错误响应不回显 token，常规应用日志不输出请求 URL；反向代理也应禁记查询字符串。

只读路由仅支持 GET/HEAD，管理 API 始终只认家庭共享账号 Cookie：把 token 搬到 `/api/*` 的查询参数不会获得权限。串流不提供字幕、浏览、扫描或进度写入，也不产生任何观看记录。源文件不可用时返回实际 409/410，不接受 Cookie 作为串流路由的授权兜底。

## 外挂字幕（06）

| 请求 | 成功响应 / 行为 |
| --- | --- |
| `GET /api/media/{id}/subtitles` | 需要会话及可用原视频；200 `{items:[{id,filename,format}]}`，无匹配项时 items 为空数组 |
| `GET /api/media/{id}/subtitles/{subtitle}` | 需要会话；校验该视频的同目录候选与当前文件指纹；200 `text/vtt; charset=utf-8`，SRT 会实际转换为 VTT |

字幕 id 是媒体标识、候选文件名和文件指纹的 SHA-256，不是文件路径，也不是授权凭证；只认当前候选集合，不能指定任意路径。读取拒绝符号链接、非常规文件和读取过程中发生的替换。候选变化返回 404，权限/读取竞态 409；格式、编码、结构、时间错误或超过 2 MiB 返回 422 JSON 错误。读取/转换不修改源字幕；详细命名/格式子集见 README。

网页预先获取并验证 VTT，再通过本地 Blob 交给原生 `<track>` 渲染，选择切换会禁用/移除旧 track、取消未完成请求并释放 Blob。CSP 仅为原生媒体增加 `blob:` 来源，脚本仍只允许同源。字幕错误不会停止视频或伪造加载成功。
