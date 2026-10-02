# 重构现状审计与验证基线

- 日期：2026-10-02（Asia/Shanghai）
- 基线：`4dffbaacf0be73846e714de048b514917a2e3a20`
- 关联方案：[spec.md](./spec.md)

## 1. 审计方法与范围

本次是全库需求与设计梳理，不是只评审最后一个提交。开始时工作区无已跟踪文件改动。

需求依据：

- `AGENTS.md`、`docs/agents/{issue-tracker,triage-labels,domain}.md`。
- `CONTEXT.md`、`docs/adr/0001-single-file-permanent-deletion.md`、`0002-host-mounted-media-directories.md`、`0003-non-expiring-read-only-stream-links.md`。
- `.scratch/core-media-library/` 的 spec、讨论、实现建议、map 及 issue 01–19；特别核对 issue 10/11/12/17/18/19 的替代要求和补充评论。
- `README.md`、`docs/api.md`、`docs/verification.md` 与提交历史交叉核实，不把早期“范围外”或“尚无测试”沿用到当前。

代码路径覆盖：启动与部署；账号/HTTP；目录/文件身份；扫描/组织/收藏/查询；三资料源及配置；匹配/缓存/演职员/人物/图片；播放/串流/进度/字幕/删除；React 页面、请求与状态、CSS；Go 测试、浏览器测试与隔离 fixture。

统计（仅用于定位维护压力，不把行数当代码质量指标）：

| 项目 | 当前值 |
| --- | --- |
| `internal/app` 生产 Go 文件 | 28 个、5,451 行、75 个 `*App` 方法 |
| `web/src` TSX 文件 | 18 个 |
| `web/src` CSS | 3,837 行；其中 `style.css` 2,278 行 |
| 主要页面文件 | `home.tsx` 712 行、`library.tsx` 583 行、`main.tsx` 433 行 |
| 测试形态 | 真实 HTTP/临时 SQLite/临时文件，三源 fixture，27 个 Playwright 场景 |

## 2. 证据与影响

以下为可由基线代码复核的结构性事实。风险推导不等同于已经复现线上事故。

### A01：单体 App 使职责与依赖交织

- `internal/app/app.go:22,49`：Config 同时包含生产设置、旧凭据导入、三源测试地址、文件根和前端资源；App 同时持有 DB、密钥、后台任务和多种互斥。
- `app.go:New/Handler` 顺序调用多个初始化方法并注册全部路由；`directories.go` 同时含建表/回填、路径安全、HTTP、后台调度。
- 影响：更改资料设置、编目或文件安全都需理解共同 App 的全局依赖。目标是收拢知识和事务 interface，不是只把方法分到更多文件。

### A02：局部读取有全库复杂度

- `internal/app/metadata.go:384` 的 `attachMetadata` 查询没有目标 WHERE，读取全部 `metadata_matches` 及未到期 payload，并逐个 JSON 解析。
- `internal/app/library.go:138` 的单媒体详情以及 `series.go:series` 同样调用该方法，所以即使只看一个文件也会处理无关作品资料。
- `library.go:56` 查询全部符合类型/目录的文件，再装配全部资料、内存搜索与系列汇总，没有分页。
- `web/src/home.tsx:220,308,324,357,375` 全量接收 items/series 后再搜索、分组、排序、选续播；每库 collections 重复过滤文件集合。
- 影响：随片库、演职员长度和媒体库数增长，放大 DB、JSON、网络和浏览器成本。本轮没有为这一条虚构延迟数字。

### A03：资料网络请求持全局关联锁

- `metadata.go:173` 的 `matchLibrary` 在 `s.operation.Lock()` 内调用最多 65 秒预算的 `matchTarget`。
- `matching.go:31` 手动确认、`metadata_sources.go:updateDirectorySources`、`metadata_settings.go:249` 设置保存也争用同一锁；TryLock 失败返回 409。
- 影响：一个慢来源使不相关目标的确认、目录策略、凭据保存显示“忙”。锁能防止旧配置落地，但把网络延迟暴露给所有写操作。不能只删锁，须以版本条件提交替代。

### A04：资料绑定缺少真实目标约束

- `metadata_sources.go:initializeMetadataStorage` 的 `(kind,target_id)` 没有到文件/电视剧的外键；`matchingTarget` 还用 `kind=series` 同时表示整部电视剧和未组织文件。
- `scan.go:removeMissingMedia` 手动按多段 SQL 清理绑定；扫描使用 `mediaMutationMu`，资料写入使用另一把 `metadata.operation`。
- `metadata.go:357` `saveMetadata` 在网络完成后读取目录策略并 upsert 绑定，没有在提交点重新验证目标存在/版本；`metadataFailure` 同样可写目标状态。
- 风险推导：扫描先移除某目标、旧资料任务后写入时，可留下无本地目标的绑定；设置/手动选择/图片刷新并发需要统一条件提交。本轮未新增测试复现该竞态，方案将其列为必须覆盖的确定性交错测试。

### A05：通用身份被 TMDB 与历史兼容塑形

- `metadata.go:16` 同时有 `SourceID int64` 和 `TMDBID int64`；读取和选图存在 TMDBID fallback。
- `matching.go` 接收并补全旧 `tmdbId`，`app.go:Handler` 保留旧 poster 路由。
- `metatube.go:98,133` 搜索调用 `localID` 写 `metatube_titles`，仅为满足整数形状；adapter 因而依赖 `*sql.DB`。
- `metadata_sources.go` 的缓存键是来源/类型/整数 ID，`metatube_titles` 唯一键是 provider/remote_id，不包含自部署地址。`metadata_settings.go` 换 URL 会替换客户端，却不形成独立资料目录空间。
- 影响：搜索具有隐藏写副作用；更换服务地址后相同字符串 ID 的身份隔离没有由模型保证。既有换地址 Token 保护应保留，不能误报成已经存在 Token 外泄。

### A06：安全的粗锁限制了跨媒体库并发

- `scan.go:123` 从遍历开始到事务结束持全局 `mediaMutationMu`；`collect` 使用 `ReadDir(-1)` 并收集完整 `[]discoveredFile`。
- `directories.go:startScan` 每个目录直接启动 goroutine，尚无全局有界扫描 worker；最终仍在同一锁上排队。
- 影响：大目录扫描的内存与发现文件数相关，也阻塞无关目录的扫描/删除。方案保留同库扫描/删除互斥，仅缩小锁范围的资源维度，不删除安全互斥。

### A07：多类图片实现重复，存在队头阻塞

- `tmdb.go/tvdb.go/metatube.go:details` 在获取资料时下载 primary；`metadata_artwork.go:115` 懒加载横图；`people.go:personPhoto` 懒加载照片，各自有缓存结构和失败处理。
- `metadataService` 的 `imageGate`/`photoGate` 分别只有一个槽，不同 key 的未缓存图片也串行。
- 图片路径、大小/类型校验、来源隔离、负缓存、取消处理已存在，不应全部推倒；应统一到一个有界按 key 合并的图片 module。

### A08：人物性能已有优化，不能误判为“仍在扫所有 JSON”

- `metadata_people.go` 已有持久人物索引；`people.go:69` 先按 source/person_key 查询关联，再处理命中的作品。
- 同时存在 payload 真源、`metadata_people_state`、人物关系和 payload 变更触发器，并在启动时回填旧/外改缓存；这是演进成本，而不是没有索引。
- 原 issue 15 报告过 5,000 作品约 2.69 秒→6 毫秒的历史测试；本轮没有重新跑该前后版本比较，不把历史数字当本轮测量。
- 方案保留索引化读取及有效关联筛选，去掉“兼容外部直接改 JSON”的运行维护成本，并继续检查查询计划。

### A09：前端状态同步由多个局部机制拼接

- `main.tsx:129` 直接读 `window.location` 决定页面；浏览导航多用整页链接，部分流程另用 history.replaceState，不是一个统一路由模块。
- `metadata.tsx:169` 每 1.5 秒轮询，初次读取 revision 就可能发 `metadata-updated`；目录页每秒轮询，不区分长期空闲。
- `home/library/series/people` 监听全局事件分别重新请求。home 有 Abort/revision 防旧响应措施，而其他页面各自维护 active 或没有同等保护。
- 影响：需要逐页面证明刷新、收藏、匹配与切页的排序；优化不应只是全站换一种名字的广播事件。

### A10：播放器保存是无界顺序 Promise 链

- `player.tsx:save` 每个快照追加到 Promise 链；`close` 等待该链；`api.ts` 未统一设置请求超时。
- 优点：避免单播放器进度请求乱序。风险：慢网络/频繁 seek 可积压，关闭等待时间不受控。替代方案必须保留顺序/最终快照，不能简单并行发送或关闭即丢弃。

### A11：样式与展示残留

- `style.css` 中有通用 sidebar/header/content；`home.css` 和 `detail.css` 再按页面类重写同一壳的布局。
- `library.tsx` 同时实现目录管理、文件详情，又 re-export `home.tsx` 的 Library。
- `rg SeriesCard web/src` 只有 `series.tsx` 的定义，没有使用者；实际首页卡片在 `home.tsx`。
- 影响：样式生效顺序、共享布局与页面职责分散。删除应基于引用/视觉回归，不凭 CSS 行数判定所有规则无用。

### A12：迁移与文档一直追加，缺少统一当前态

- `initializeLibrary/Organization/Discovery/MetadataStorage/MetadataSettings` 分别建表、查列、回填或导入。
- 原 spec 的单源/不含收藏、成功扫描可保留缺失等约定已被 issue 替代；README/API 同时包含多轮升级说明。
- 影响：后续实现者可能从不同入口得到不同产品范围。重构 spec 已明确优先级；旧历史文档不改写，实施后重写当前使用说明。

## 3. 必须继承的工程资产

不是“现有代码全部劣化”。以下设计已有真实价值，应迁移到新的 seam 并保留行为验证：

- `playback.go:openMedia`、`directories.go:openDirectory`、`deletion.go` 的 bounded handle、指纹/父目录核对、非递归 unlink、取消后的独立索引核对与部分成功响应。
- 完整扫描后一次发布、失败保留旧库、成功清理缺失、保守季集识别、文件版本独立。
- 会话 Cookie 与不可猜测只读串流能力分离，同源校验、CSP、密码哈希与登录限流。
- 资料设置加密、revision、显式清除、换地址不转移旧 Token；上游错误脱敏、受控图片地址、来源隔离和退避。
- 人物索引、空名单/故障区分、同名人物不强行合并、过期资料不供给。
- 真实 Range/VP8 播放、字幕渲染、删除权限/SQLite 锁失败、跨会话状态、重启与桌面/窄屏浏览器覆盖。

## 4. 本轮验证记录

环境：macOS arm64；Go `1.25.12`；Node `26.7.0`；npm `11.19.0`；使用本机 Chrome。PATH 未找到 Docker CLI。

| 命令 | 本轮结果 |
| --- | --- |
| `npm test`（含 TypeScript/Vite、`go test ./...`、Playwright） | 通过；27 个浏览器场景全部通过，浏览器阶段 1.8 分钟 |
| `go test -race ./...` | 通过；应用包 430.992 秒，无 race 报告 |
| `go vet ./...` | 通过；在 race 通过后执行，组合命令退出码 0 |
| `npm run format:check` | 通过；现有前端、测试、脚本及 Compose 格式正常 |
| 新文档链接/Markdown 围栏/空白/工作区范围检查 | 通过；含对未跟踪文档的 `git diff --no-index --check`；只有 `.scratch/architecture-refactor/` 新文档，无已跟踪文件改动 |

构建测得现有主 JS 264.49 kB / gzip 81.97 kB、CSS 54.79 kB / gzip 12.04 kB；仅是构建产物体积，不能证明页面性能。普通 Go 应用包测试本轮完成耗时 214.036 秒，也不是请求延迟基准。

测试通过现有隔离 harness 使用临时媒体/DB 和本地上游 fixture；没有读取真实凭据、操作用户媒体、改动现行服务或执行真实第三方请求。构建和 Playwright 可能刷新 gitignored 的 dist/test-results，不属于源代码变更。

未验证项：目标架构尚未实现；50,000 文件的目标性能尚未实测；本轮不证明真实 TMDB/TVDB/MetaTube 服务联通、Docker 部署、NAS 语义、IINA/VLC/PotPlayer 实机、其他音视频编码或恶意宿主机竞态安全。
