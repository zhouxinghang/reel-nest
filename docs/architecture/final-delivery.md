# 全量重构最终交付（issue 06）

日期：2026-10-02（Asia/Shanghai）。起点 `f408ed1`。本文是当前实现说明；阶段 01–05 交接及 `docs/verification.md` 中较早记录保留为历史，不作为旧入口仍可运行的承诺。

## 清理与引用审计

| 删除项 | 消费者处理 / 保留边界 |
| --- | --- |
| `internal/app` 旧业务、SQL、handler、provider、迁移初始化、整数 MetaTube 映射及测试 | 仅保留 `runtime.go` 装配；新 catalog/metadata/playback/mediafs/store/transport 承担真实业务；没有改名保存旧单体 |
| `optionalSecret` / `LegacyMetadataCredentials` / `ImportLegacyMetadata`、旧资料源环境参数 | 生产与测试都通过当前 settings API；家庭账号 `_FILE` 保留并由正式 CLI 浏览器场景验证 |
| 旧目录名称/收录时间/组织/演职员/人物/图片回填，旧 tmdbId/无 source 路由 | 新空 schema 唯一初始化；正常重启、有效资料刷新/可选重试和 CAS 保留；旧路由 404，未知字段拒绝 |
| `tests/legacy-server` / `tests/server` / 27 个旧 UI 场景实现 | 97 项审计记录保留：4 项纯历史迁移退役，93 项追踪到可执行新测试，逐条 evidence 由 baseline:check 核验；不是删除后降低门禁 |
| 顶层旧前端页面/请求/手写接口/metadata 事件/卡片/re-export/CSS | 当前 app/features/shared/generated 无引用旧顶层模块；单一 CSS Modules 体系，只有 reset/token 全局样式 |
| `vite.legacy.config.ts` / `vite.runtime.config.ts` / `runtime.html` / 多构建脚本 | 一次 production build，测试装配与正式 CLI 都嵌入同一份静态资源；`/__native-player` 仅为测试消费者，不是另一套 UI |
| 三份旧资料源 Compose / 旧组织升级脚本 | 删除；保留只读 `compose.media.yaml` 与显式读写 `compose.media-write.yaml`，不自动开启删除权限 |
| 旧 README/API 当前态叙述 | 重写为已实现行为、安全边界、新空目录、拒绝旧库、备份切换/回退步骤；历史 issue/旧验证记录不改写 |

`npm run architecture:check` 核验生产模块导入方向、provider 仅依赖 model、SQL 仅出现在 store、App 只有装配文件、旧兼容符号与顶层旧前端不复活。人工核对 `runtime.go` 仅初始化/关闭，provider 无数据库，HTTP 仅 DTO/路由/错误/鉴权及流输出，文件安全/删除事务分别留在 mediafs/playback/store。第三方 `kin-openapi/routers/legacy` 是该库的路由匹配器名称，不是 Reel Nest 历史路由或兼容业务。

修复构建审计发现的 Dockerfile 缺少 `COPY api ./api`（后端编译依赖嵌入契约）。本机没有 Docker CLI，仅修正构建输入并完成 Linux 交叉构建，**没有将其记为容器实测通过**。

## 回归迁移策略

原 97 项的需求编号、原测试名称/文件/分类都保留在 [regression-inventory.json](regression-inventory.json)。`state=migrated` 必须至少指向一个仍存在的 Go/Playwright 测试或 UI suite；`retired` 只准用于原先标记的 4 个纯迁移项。混合升级测试只剥离构造旧表/导入步骤，保留正常重启、有效刷新、失败重试、状态和并发约束。层级迁移明确记录，不宣称与原 27 个浏览器场景逐行一一同构。

- 新增 4 个浏览器场景将 TMDB/TVDB 电影、电视剧、人物、本地作品、图片、窄屏、上游回退、可选资料失败与真实 VP8 播放串起来；MetaTube 继续由完整链路覆盖。
- 三源仅替换外部 HTTP origin；扫描、SQL、授权、资料提交、实际文件操作不 mock。测试 runtime 的 HTTP seam 拒绝未列入 fixture 的 origin；生产使用默认受控 HTTP client。
- 真实权限/目录替换/数据库写锁失败由 Go HTTP + 真实临时文件/SQLite 测试承担；删除结果提示/不重试由 UI suite 和“真实 unlink 后丢响应”浏览器测试承担。不会将模拟响应包装为实际 EROFS/NAS 故障。
- 新增 retired-contract 测试：旧路由不提供别名、API 不回退 HTML、旧字段/尾随 JSON/超限体不被接受。
- 新增 20 秒慢源真实 HTTP 基准和 50,000 个真实临时文件的有界遍历，补齐最终量化证据。

## R01–R12 交付证据

| 需求 | 主要可复跑证据 |
| --- | --- |
| R01 身份/部署 | identity 会话/限流；integration runtime 生命周期/密钥；store 新库、旧库拒绝、WAL；production.spec 正式 CLI `_FILE`/重启/拒绝旧目录；容器未测 |
| R02 媒体库 | HTTP library validation/分页/目录失败，store library overlap/revision，mediafs containment，UI 建库/改名 CAS/空库 |
| R03 扫描 | catalog 跨库有界 worker、取消/部分读取失败、进程崩溃恢复；store 分批 staging/原子发布/状态保留；真实 50k 文件遍历 |
| R04 组织 | ConservativeEpisodeNaming、organization subject 变化、HTTP 数字季集/多版本与浏览器 URL 季集分页 |
| R05 浏览 | catalog read/group/cursor 测试、固定详情查询数、三档真实 HTTP 基准、UI URL/前进后退/独立分组/DOM 上限 |
| R06 家庭状态 | store 重扫身份/收录时间/收藏独立，HTTP shared progress，runtime 重启/流密钥，UI 真实播放续播/ended |
| R07 匹配 | 三源 provider contract、metadata 唯一匹配/回退/CAS/手动选择/到期/刷新、HTTP候选与新版三源浏览器链路 |
| R08 人物/图片 | metadata people/assets TTL/来源空间、singleflight 及全局4/同源2约束、provider 图片安全、三源真实 PNG 与本地作品 UI |
| R09 设置 | settings 加密/篡改/并发保存/回滚/重启、HTTP 严格输入、UI 草稿冲突；20 秒慢源时 settings 真实 HTTP p95 |
| R10 网页播放 | HTTP GET/HEAD/Range/If-Range/条件请求/权限/文件替换，原生 VP8 限速/拖动，Player 状态与有界写队列/3秒关闭 |
| R11 外部/字幕 | HTTP 凭证与会话隔离/重启、字幕 UTF-8/分页/版本；浏览器原生字幕及会话退出后串流、协议编码/复制兜底；实机播放器未测 |
| R12 删除 | mediafs 精确 unlink/父目录/权限，playback 取消/未知/关闭后核对，HTTP 真 SQLite 写失败/扫描修复，UI 二次确认/取消/响应丢失 |

所有测试仅使用隔离临时媒体和 DB；没有用户媒体、真实凭据或真实外部来源访问。

## 最终自动化与性能结果

### 自动化

- `npm test`：契约生成检查、生产 TypeScript/Vite、测试 TypeScript、37 个前端 unit/hook、全量 Go 与 **16 个 Playwright** 通过；另跑 `go test -count=1 ./...` 禁用结果缓存通过。
- `go test -race -count=1 ./...`：全量通过。`go vet ./...`、生产 Go 构建、`GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build` 通过。
- `npx playwright test tests/runtime --repeat-each=3`：**48/48** 通过（约 2 分钟）。16 场景包括 6 个 catalog、4 个交互、4 个三源扩展、1 个原生视频、1 个正式 CLI。
- `npm run baseline:check`：97 个原始记录的名称/分类/需求/备注保持，93 个替代证据存在、4 个纯迁移退役；`npm run architecture:check` 通过。
- `npm run format:check`、Go gofmt 与 `git diff --check` 通过；没有安装新的依赖。最终交付还核对依赖 tidy 无差异。
- 目视检查桌面媒体页、390px 删除对话框、TVDB 电视剧页面；自动化核验键盘及窄屏无水平溢出。未宣称逐像素复刻。

### spec §11 查询性能

命令见 README。原始 [catalog-http-final.json](catalog-http-final.json) 含每端点 firstRead/p50/p95、响应字节、返回行数、SQL/EXPLAIN 与详情查询数。环境：**Apple M5 Pro、18 CPU、48 GiB、darwin/arm64、Go 1.25.12、Node 26.7.0**。10 个媒体库；1,000 / 10,000 / 50,000 文件，1,000 / 5,000 / 5,000 资料目标，每目标 100 演员 + 50 导演。

每端点单独重开 SQLite 后先测首读，再测 30 次连续真实 loopback HTTP；含家庭鉴权与序列化。先完成浏览器/race，再单独运行查询基准与两个扫描规模验证，没有与其他测试并跑。**OS 页缓存未清除，firstRead 不叫冷盘；这些是本机工程测量，不是生产 SLA。**

热 p95，单位 ms：

| 文件数 | 首页 | 单库50项 | 分组 | 最近添加 | 文件详情 | 人物50部作品 |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 1,000 | 5.360 | 1.440 | 5.314 | 1.901 | 1.063 | 4.289 |
| 10,000 | 21.772 | 3.803 | 30.327 | 7.245 | 0.996 | 16.377 |
| 50,000 | 86.301 | 7.193 | 65.567 | 24.784 | 0.976 | 16.440 |

全部 9 个端点/3 档热 p95 ≤200 ms。50k 档首页 **54,471 B**、50条单库页 **25,483 B**，低于256 KiB；单文件详情 **7条 store SELECT + 1条会话鉴权**、只解析本目标150条演职员、无无关演职员解析。查询计划仍可能含轻量扫描/临时排序（任意子串与动态标题等），不声称全查询 O(page size)。

### 慢源、图片、扫描与前端上限

- [slow-provider-final.json](slow-provider-final.json)：一个真实 provider HTTP 请求持续 **20.002秒**；在其中15秒内，各做30次已登录本地首页/进度/设置 HTTP。p95 分别 **3.850 / 1.595 / 1.123 ms**，设置保存≤200ms；单文件/单库真实SQLite，不是50k档复合负载或生产SLA。单独复跑，未与浏览器/race重叠。
- 图片的 `TestAssetsSingleflightCancellationHitAndFailureBackoff`、`TestAssetsGlobalAndSourceBoundsAndStaleConfig` 全量/race通过：同key合并、4全局/2同源、缓存命中与慢图隔离。浏览器确认三源实际PNG解码及失败不阻断播放。
- `REELNEST_LARGE_WALK=1 go test ./internal/mediafs -run '^TestLargeWalkRemainsBounded$' -count=1 -v`：**50,000个真实临时视频文件**，196次回调、最大256项，遍历22.818秒；整个测试含创建/清理36.56秒。代码逐目录 `ReadDir(256)` 并立即StageScan，没有全库文件清单；目录递归深度也有上限。
- `go test ./internal/store -run '^TestLargeScanUsesBoundedStagingPages$' -count=1 -v -args -scan-fixture-files=50000`：50,000行分批暂存 **655.121ms**、集合式发布 **2.978秒**，最大256行一批；这不是额外一次50k真实磁盘扫描。失败/取消零发布与跨库并发由catalog/store专项回归证实。
- 前端单页/每组数据有界、URL驱动、查询取消、空闲30秒/隐藏暂停、单在途+最新快照和3秒关闭由37项unit/hook与浏览器反复验证，不按浏览器测试总数量推断覆盖。

## 部署与未验证项

- 未执行部署、切换 DATA_DIR、弃用旧卷、重启现有服务或推送远端；只交付代码与本地 commit。
- Docker CLI 不可用，容器非 root、只读根、只读/读写挂载、容器持久化 **未实测**。Linux amd64 交叉构建不等于运行容器。
- 未调用真实 TMDB/TVDB/MetaTube；fixture 成功只证明受控协议与应用行为，不证明真实凭据/网络/资料覆盖。
- 未验证真实 NAS、EROFS 只读挂载、跨设备 LAN、IINA/PotPlayer/VLC 实机、其他浏览器/编码/音轨。VP8 fixture 无音轨，不能宣称音频兼容。
- POSIX 最终复核到 unlink 的外部竞态、NAS 可能不可中断、长期串流凭证泄露、关闭进度 best-effort 的边界保留。永久删除不可恢复。
- 切换/回退按 [README](../../README.md) 独立执行：先停旧实例并完整敏感备份；新空目录重新配置/扫描；回退用旧程序+封存旧数据，新旧状态不合并。不得以真实视频做删除冒烟。
