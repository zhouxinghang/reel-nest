# 当前应用 HTTP 契约（01–03）

应用与网页同源。所有写入请求为 `application/json`，无参数 POST 发送 `{}`；错误返回 `{"error":"可显示的错误说明"}`。对跨站浏览器写入请求返回 403。除健康检查和登录外，API 均要求家庭共享账号会话；串流授权尚未实现。

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

媒体文件字段：`id / directoryId / directoryPath / filename / relativePath / path / kind / size / modifiedAt / available / matched / episode / organizationReason`。大小为字节，修改时间为 Unix 秒；`available` 是最近一次完整扫描的文件版本状态，不是实时探测结果。当前尚未接入影片资料，`matched=false`；即使不可用仍可查看记录和源文件路径。

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

未登录 API 返回 401；受保护页面 `/library`、`/directories`、`/media/{id}`、`/series/{id}` 引导至 `/login`。电视剧页面使用 `?season=2` 选择第 2 季；不存在的季显示明确提示。前端资源不包含秘密。没有视频读取、进度、字幕、TMDB 匹配或删除接口，不应将文件详情响应当成播放验证。

扫描在真实文件系统根句柄内进行，拒绝符号链接并检测根目录替换；失败扫描不发布部分索引。应用数据（SQLite，WAL 模式）独立保存；按目录和文件版本建立稳定随机文件标识。身份识别的精确边界见 README。
