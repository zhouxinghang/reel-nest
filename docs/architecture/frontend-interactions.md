# 阶段 05：前端交互与正式源码入口

日期：2026-10-02（Asia/Shanghai），基线 `36f4381`。对应 `.scratch/architecture-refactor/issues/05-frontend-playback-interactions.md`。

本文接续此前的 `frontend-foundation.md` 与 `playback-backend.md`，它们是阶段性历史记录。**正式源码已改为新前端 + 新 runtime；这不是部署或用户实例切换。** 完整门禁结果以 issue 05 的最终 Answer 为准。

## 正式入口与数据边界

- `web/index.html` → `web/src/app/main.tsx`；React Router、TanStack Query、生成契约请求客户端。生产包不引用旧 `web/src/main.tsx`、旧手写接口、全局 metadata 事件或旧全局 CSS。
- `cmd/reelnest` → `app.NewRuntime`，通过 `RuntimeConfig.Assets` 和 `internal/web.Handler` 同步提供新版 API/静态资源。API、串流、健康检查错误不回退 HTML；前端路由刷新返回 SPA，静态资源缺失返回 404。保持 CSP、nosniff、frame-ancestors、no-referrer，允许本源视频和字幕 Blob。
- `npm run build:web` 是生产 TS/Vite 构建；`npm run build` 额外执行契约检查及两套隔离回归资源构建。Docker 的 Node stage 只运行 `build:web`，不尝试在无 Go 的 Node 镜像运行契约 Go 测试；容器仍未实测。
- 正式 CLI 不再调用 `ImportLegacyMetadata` 或读取旧资料源环境变量。家庭账号及 `HOUSEHOLD_PASSWORD_FILE` 保留；资料源通过新版设置页面配置。
- 必须使用**新空 DATA_DIR**，或本版本正常初始化的目录；旧目录/不完整初始化/缺失密钥均拒绝打开，不导入、修复或清空。新旧状态不可混用，旧 URL/串流链接不承诺兼容。部署/备份/回退按 spec §12.2 单独执行。

## 管理、扫描与任务

- `/libraries` 使用生成 `LibraryCreate/LibraryPatch`：允许根目录提示、名称/路径/电影或电视剧、三源勾选及优先级、自动首次扫描、显式重扫、按库刷新影片资料。
- 编辑表单独立保存 baseline/draft。409 后保持草稿且禁止保存；显式读取当前有界页成功、找到目标并采用其 revision 后才可再次提交。后台刷新不会重置输入。重命名只改媒体库展示名，不改目录。
- 媒体库每页 50 项；认证壳的 `ScanProvider` 是唯一扫描轮询位置，最多监控**最近访问管理页的 50 个 scan ID**，不是所有库的全局任务计数。每个活动扫描 1.5 秒，完成停止，读取失败 30 秒，隐藏暂停/取消。切换管理分页替换整个监控集合，不积累无限订阅。
- 资料任务仍只有一个 `TaskStatus`：活动 1.5 秒、空闲/失败 30 秒、隐藏暂停。首次读取只建立 revision 基线，后续变更只刷新当前订阅的有界投影。
- `/tasks` 是按需分页的失败目标列表（50 项），不另建轮询器。全局/按库刷新仅显式提交，不把资料获取当文件扫描，也不自动重放失败 mutation。

## 匹配与展示

- 文件和电视剧都能打开候选弹窗；按来源/查询消费版本化 `CandidatePage`，提交原样 `RemoteRef`、subjectRevision 和 expectedRevision。搜索不写关联，不把部分候选当唯一匹配。
- 确认前/确认后取消受影响读取。确认值更新相同 subject 的 Match，卡片标题/单集显示语义仍由服务器投影决定，随后定向失效详情、当前媒体库/全局有界列表、人物/作品。未关联的媒体库和设置不被失效；后台旧响应不能覆盖确认结果。
- 409/CANDIDATE_STALE 禁止继续选择旧候选，要求重新搜索。取消/错误不清除先前选择，设置/选择 mutation 均不自动重试。
- 详情展示海报/背景、年份/简介、文件信息、系列季/单集、演职员与人物作品；图片只走认证的 `/api/assets/{id}/content`，失败隐藏图片，不回退任意外链。保留深色/绿色、2:3 卡片、首页横向媒体栏、共享收藏入口、CSS Modules。
- 原生 `<dialog>` 提供焦点约束、Escape、调用点焦点恢复；桌面/390px 保持可操作。不是逐像素复刻旧全局样式。

## 播放、进度与字幕

- `usePlayer` 管理 checking/ready/playing/paused/blocked/failed/closing。先预检并读取共享进度，恢复 seek 完成后调用 play；原生 autoplay 拒绝进入 blocked，用户手动播放仍可恢复。读流故障与不支持解码分别提示，不承诺浏览器全编码支持或转码。
- 原生 ended 事件才允许 completed=true；接近结尾、暂停、seek 不自行推断完成。从头播放忽略旧位置，已完成内容再次播放从头开始。
- `ProgressWriter` 最多一个在途写入和一个可替换的最新快照。10 秒、pause、seek、隐藏时触发，不建立 Promise 队列，不自动重放失败写入；其他浏览器仍由服务器按最后有效接收更新。
- 关闭先暂停并提交最后快照，等待最多 3 秒；超时取消请求并丢弃待写槽，允许退出并保留“保存未确认”提示。取消传输不能证明服务器未接受请求；离开页只是 best-effort，不能保证保存必达。未开始播放不写进度，无有限有效时长不谎报保存。
- 字幕每页 50 项；列表/内容消费 AbortSignal。切换、关闭、翻页、卸载取消旧读、移除 track 并释放 Blob URL。服务端仍负责 UTF-8 SRT/VTT 验证/转换/权限/文件版本，前端不改源文件。
- 外部串流地址提前获取；IINA 按 query 参数整体编码、PotPlayer/VLC 保留完整已编码 HTTP URL。剪贴板失败显示并选中手动复制地址，提示链接不过期、仅单文件只读、不会更新网页进度、字幕自备、localhost 不能跨设备。协议 URL 正确**不代表**应用已唤起或实机已播放。

## 删除的结果语义

1. 独立 preview 读取准确绝对路径、警告和有效期；用户再次勾选后才允许 POST `{confirmation, confirm:true}`。
2. 过期禁止提交；Escape/取消只关预览，没有删除请求。后台仍验证目标/父目录/版本/期限和库变更租约。
3. HTTP 200 分别解释 `deleted/updated`、`deleted/pending`、`not_deleted`、`unknown`。已删但索引待核对不假称失败后可重试；网络断连/非 JSON/超时一律是客户端结果未知，不当作“未删除”。
4. mutation 不重试/不离线排队；一次提交后终态只提供结果关闭。提示检查文件/重扫，不提供盲目重试。会话/验证等明确前置 4xx 错误提示未执行并要求重新预览。
5. 本次 UI 真实 unlink 仅用于临时 fixture；字幕、相邻视频和旁边文件保持不动。POSIX 外部文件系统竞态和 NAS 阻塞边界仍见后端交接/ADR，不声称原子 compare-and-unlink 或应用内恢复。

## 验证分层与边界

- unit/hook：生成请求、收藏/匹配迟到响应、草稿冲突、分页 GC、任务/扫描频率和隐藏、单在途 + 千次快照合并、3 秒关闭、10 秒/pause/seek/hidden/ended、协议编码、字幕 Abort/Blob、删除到期/取消/部分成功/未知结果。
- `tests/runtime/catalog-ui.spec.ts`：新 UI URL/分页/分组/搜索/收藏/季集/设置 CAS/401/390px/键盘，保留此前五项并增加真实解码失败与失败目标分页。
- `tests/runtime/interactions-ui.spec.ts`：真实登录→建库→扫描→搜索/收藏→设置来源→匹配→人物/作品/图片→VP8 播放/续播/从头/ended/字幕→复制兜底→取消/确认删除；另测真实库/匹配 CAS、任务页、autoplay 拒绝可恢复、慢进度关闭，以及真实 unlink 后故意丢掉响应时只显示未知且不重试。
- 上述最后两类错误测试只替换浏览器策略/传输时机；正常链路不 mock 身份、SQL、文件安全、扫描或匹配事务。资料服务只使用本地受控 MetaTube HTTP fixture；正常影片图片由本地 fixture 提供，不请求真实来源或用户媒体。
- `tests/runtime/playback.spec.ts` 保留原生视频独立的限速 Range/实际解码/字幕/无会话串流验证；后端已有 Go 测试保留 GET/HEAD/单多段/条件请求、版本/权限/取消、部分删除与真实 SQL 故障。
- `tests/runtime/production.spec.ts` 真正启动本次构建的 `cmd/reelnest`：新嵌入 UI、新 API、家庭密码 `_FILE`、设置不读旧环境、正常停止重启、拒绝临时旧/无效目录且不改文件。不是只跑测试装配宣称正式入口已验证。
- 本机 Docker 不可用。真实 NAS/EROFS、真实三源、其他编码/浏览器、跨设备和 IINA/PotPlayer/VLC 实机都未验证。

## 06 可核实清理清单

这些文件目前**仅历史回归仍消费**，不是生产兼容入口；06 应逐项保留/移植安全和正常重启证据，再删除实现，不整体删除测试：

| 旧路径 / 临时材料 | 当前消费者 / 新替代 |
| --- | --- |
| `internal/app` 除 `runtime.go` 外的旧业务装配/handler/DB/迁移等 | `tests/legacy-server`、`tests/server` 和旧 Go 用例；新 module + transport 已替代正式 CLI |
| `cmd/reelnest.optionalSecret` | 仅原基线单测引用，正式 run 不调用；06 删除 helper 与纯迁移单测，保留新的生产 `_FILE` 验证 |
| `web/src/main.tsx` 及顶层旧页面/请求/样式/re-export | 仅 `vite.legacy.config.ts` 的测试构建；生产入口只引用 app/shared/features |
| `tests/legacy-server` / `tests/server`、`build:legacy-test`、`.reelnest-test/legacy-web` | 临时保留 27 个旧 UI 场景；对照 regression-inventory 按安全/功能/纯兼容分类改写后清除 |
| `web/runtime.html` | 已无构建引用，可删除；`vite.runtime.config.ts` 暂时复制相同 production index 给 runtime fixtures，并非另一套产品 |
| `scripts/test-server.mjs` 的旧主进程/三源 fixtures | 旧回归仍用；新交互也使用 MetaTube fixture，不能误删受控上游数据 |
| Compose 旧资料源变量、导入/升级脚本、当前态 README/API 的旧内容 | 按 spec §12.1 与 06 范围处理；本次只加入口切换警告，不假称整个清理已完成 |

06 仍需完整清理、最终性能复测与当前态交付说明；05 通过不等于 06 已完成，更不等于允许切换真实 DATA_DIR 或重启用户服务。


## 最终门禁

- `npm test`：37 unit/hook、全量 Go、39 Playwright 全通过（27 历史 UI、10 新 UI、1 原生视频、1 正式 CLI）；浏览器 2.2 分钟。
- `go test -race -count=1 ./...`：全量通过，旧 app 433.907s，playback 3.199s，HTTP 30.010s，无 race。
- `npx playwright test tests/runtime --repeat-each=3`：36/36，通过，1.9 分钟。
- 最终 `go vet`、本机构建、Linux amd64 交叉编译、测试 TS、格式、97 项基线、diff 检查通过。无 Docker/实机播放/NAS 验证，也没有部署、推送或用户数据操作。

以上时长不是服务性能 SLA；完整追踪见 05 Answer，06 已解除依赖但尚未认领。
