# 05：前端、播放与交互切换

Status: claimed
Blocked by: 03, 04

Parent: [全量重构技术方案](../spec.md)

## 目标

将前端整体切到新契约，统一路由、服务端状态和页面组织；完成播放、进度、字幕、外部串流与两步删除的后端/前端集成，不遗漏非资料链路。

## 范围

- 使用 React Router / TanStack Query，将媒体库、分类、搜索、排序、视图、季与分页放入 URL；表单草稿/弹窗保留局部状态。
- 页面消费生成类型和有界 DTO，统一 Abort、超时、错误、401、query keys 与精准失效；移除 metadata-updated 广播和重复轮询。
- 基于任务 02 的 mediafs 实现/迁移 playback module 的预检、Range、进度、串流、字幕与两步删除 HTTP；保持 R10–R12 全部安全语义。
- 提取播放器状态机与有界最新进度合并，删除结果不能按普通可重试网络失败处理。
- 按功能收拢 UI 与 CSS Modules，保留现有视觉方向、共享卡片、键盘操作及窄屏布局。

## Acceptance criteria

- [ ] 登录、建库/扫描、设置、分类/搜索/收藏/最近添加、文件/系列/人物页面均使用新版 interface。
- [ ] 刷新及前进后退保持 URL 状态；旧响应不覆盖已确认收藏/匹配；设置冲突保留草稿，取消请求不误报故障。
- [ ] 运行期间有界轮询，空闲/隐藏页面不持续高频请求；首次状态读取不额外拉取整库。
- [ ] GET/HEAD/单段多段 Range/If-Range、真实 VP8、续播/从头播/ended/自动播放受阻及错误分类通过。
- [ ] 同播放器最多一个在途进度请求和一个最新待提交快照；关闭保存等待有界且诚实提示结果；跨浏览器仍按最后有效接收更新。
- [ ] IINA/PotPlayer/VLC 协议编码及复制兜底、独立只读串流、字幕转换/切换/关闭/Blob 释放与权限分离通过。
- [ ] 删除有准确路径、二次确认、有效期/目标版本核验；取消不删，单文件实际删除不伤相邻资源；已删/索引失败/结果未知不被误报或自动重试。
- [ ] 原有桌面/390px/键盘场景通过，卡片 DOM 与分页上限相关，不保留全库数组。
- [ ] 快速 unit/hook 测试与真实 Playwright 集成均通过；不声称外部协议唤起等于实机播放成功。

## 验证与交接

运行契约生成检查、TypeScript/Vite、unit、Go playback/deletion/race 和全套 Playwright。测试仅使用临时媒体与 fixture。完成后向任务 06 提供死代码/旧路由/旧测试的可核实清理清单。

## Comments

- 2026-10-02：认领 05；基线 `fc7b4c5`，工作区干净，03/04 已 resolved。本轮先交付新 playback module 及播放/进度/串流/字幕/两步删除 HTTP 集成与回归；新前端整体切换、播放器队列及浏览器验收完成前保持 claimed，不解除 06。仅用隔离临时媒体/数据库，不切换 CLI/用户 DATA_DIR，不重启或部署服务。

## Progress — 播放与文件操作后端增量（2026-10-02）

**本轮只交付 05 的后端部分，整票未完成，Status 保持 claimed。** React Router / TanStack Query、生成类型页面切换、播放器状态机/进度合并、CSS Modules 和新 UI 交互验收尚未完成；不勾选上面的复合验收项，不解除 06 的依赖。完整消费与安全边界见 [后端交接](../../../docs/architecture/playback-backend.md)。

### 已完成

- [x] 新 `playback.Service` 与 `app.NewRuntime` 装配：播放预检、原始内容、家庭共用进度、独立只读串流、分页字幕、两步单视频删除的 HTTP/DTO/权限和错误分类。
- [x] 同已核对句柄 GET/HEAD、单段/多段 Range、If-Range、条件 304/412；进度有限值/完成标记验证、重启保留、文件版本替换后旧身份/进度/串流失效；读取或串流不写进度。
- [x] 字幕按原命名规则安全枚举、最多 100 项分页及签名/版本/TTL 校验、2 MiB UTF-8 SRT/VTT 验证与转换；复核 inode、视频与父目录，拒绝替换/符号链接/特殊文件，不改源文件。
- [x] 有时效且绑定 ID/路径/父目录/含 ctime 版本的确认，二次显式 `confirm:true`，扫描共用每库租约；取消/到期不删，unlink 后脱离取消且最多十秒核对索引，关闭等待核对结束。
- [x] 真实单文件删除及邻接资源保留；真实 SQLite 触发器制造索引失败仍返回 `deleted/pending`，unknown 故障注入不谎报成功或自动重试；成功扫描校正/清理。
- [x] 15 个新增 Go 顶层测试、已有 runtime 重启测试扩展；新增原生视频浏览器 fixture 使用新 API/生成类型，实测 VP8 解码、限速拖动产生新的 Range、字幕和注销后的无会话串流解码。

### 实际验证

| 命令 | 结果 |
| --- | --- |
| `npm test` | 通过：契约生成、生产前端 TypeScript/Vite、全量 Go、28 个 Playwright 场景（27 个旧 UI + 1 个新 runtime 原生视频，1.9m）；旧 app Go 包 216.321s |
| `go test -race -count=1 ./...` | 全量通过，无 race；旧 app 435.812s，playback 4.462s，HTTP 28.688s |
| `go test -race ./internal/playback ./internal/mediafs ./internal/transport/http ./internal/integration -count=3` | 最终专项三轮通过：5.536 / 9.478 / 81.393 / 9.143s；包含全量启动后新增的父目录枚举/重扫替换回归 |
| `npx playwright test tests/runtime/playback.spec.ts --repeat-each=3` | 3 次通过（10.7s）；每轮独立临时目录；实际解码与字幕，不是只检查按钮或 HTTP 200 |
| `npm run contracts:check` / `npm run baseline:check` | 通过，生成类型无漂移，97 个旧基线声明保留 |
| `go vet ./...` / `go build ./...` / `GOOS=linux GOARCH=amd64 go build ./...` | 通过；Linux 仅交叉编译 |
| `npm run format:check` / `gofmt -l` / `git diff --check` | 通过 / 无输出 |

以上秒数是包/测试运行总时长，不是接口延迟或性能 SLA。全量测试与部分专项并发执行；最终只补充测试和文档，没有在全量门禁后改变生产实现。

额外的 Playwright 脚本独立 `tsc --types node` 检查因仓库缺少 `@types/node` 返回 TS2688，未列为通过，详情见交接。它不是现有 `npm test` 门禁；生产前端 TypeScript 已通过。前端切换及其测试基础仍属于本票未完成部分。

### 接续边界

1. 继续 05 的前端统一路由、Query cache、请求取消/超时/401、URL 分组/分页和有界状态订阅；本轮没有安装 Router/Query 依赖。
2. 实现播放器状态机、单在途+最新快照、最多三秒关闭保存反馈，以及字幕 Blob 生命周期、外部协议编码/兜底和两步删除“结果未知”交互。后端进度事务不能代替前端队列验收。
3. 页面/卡片/CSS Modules、桌面/390px/键盘和整套新 UI Playwright 通过后，才能完成本票、切换装配并解除 06。新原生 `<video>` fixture 只证明后端媒体/字幕可被 Chrome 实际消费。
4. 06 清理候选与替代证据已写入交接；暂不删旧 `internal/app` 的播放/串流/字幕/删除实现与旧测试，不改写当前生产 README/API 文档。

未修改 schema、CONTEXT 或既有 ADR；未迁移/导入/清空现有数据，未读取真实凭据或操作用户媒体，未切换 CLI/前端、部署或重启服务。Docker CLI 不可用；Linux 交叉编译不等于容器运行。真实 NAS/EROFS、三类资料源、跨设备及 IINA/PotPlayer/VLC 实机未验证。

## Progress — 新前端路由、查询与基础页面增量（2026-10-02）

**继续推进 05，基线 `de2e68b`。本轮仍是增量，Status 保持 claimed，不解除 06。** 新代码只在隔离 runtime UI 入口验证；正式 CLI/旧嵌入前端没有切换。完整边界、使用方式与后续清单见 [前端基础交接](../../../docs/architecture/frontend-foundation.md)。

### 已完成

- [x] 安装并锁定 React Router / TanStack Query / Vitest / Testing Library，按 app/shared/features 组织新代码及 CSS Modules，保留深色/绿色设计 token、2:3 海报比例和 390px 布局。
- [x] 新版家庭身份、首页、单库/全局分组分类搜索、最近添加/继续观看、收藏、文件只读详情、电视剧季/单集、人物/作品分页、三源设置消费生成契约。人物/来源图片的真实新 UI 联动仍待专门验收，不把组件存在当成全链路通过。
- [x] URL 保存媒体库、分类、搜索、排序、视图、季及分页；普通列表 50 条，全局分组最多 10×10 条。列表页离开立即 GC，返回按 URL 重取；不加载全库数组或在客户端另行聚合/排序。
- [x] 生成路径/方法/参数/请求/响应类型，统一 15 秒超时、Abort、结构化错误及 401 返回原站内 URL；会话切换取消读写请求并清理家庭缓存。非 JSON 不回显代理原文，主动取消不提示故障，mutation 不自动重试或离线排队重放。
- [x] 收藏采用服务器确认值；写前/确认后取消受影响读取并修补有界缓存，恢复被取消的首次读取，仅失效收藏成员资格页。回归证明写期间开始的旧读不会覆盖确认值，无关媒体库/设置不被失效。
- [x] 设置版本冲突保留草稿，显式读取新版本后才允许再次保存；后台 Query 更新不覆盖表单。实测真实 CAS 冲突和草稿再次保存，未用 mock 绕过事务。
- [x] 认证壳单一资料任务订阅：运行 1.5 秒、空闲/失败 30 秒、隐藏暂停并取消在途请求。首次状态读取不触发首页/整库重载。后续全局 revision 仅刷新当前订阅的有界投影，不声称按未知的 subject ID 精确失效。
- [x] 新增 21 个快速 unit/hook 测试、3 个编译期错误契约断言、5 个真实新 UI Playwright 场景；新增测试独立 TypeScript 检查并纳入 `npm test`，解决上一增量缺少 Node types 的工具缺口。历史 Playwright 未全部纳入独立 TS 配置，不声称已完成其类型迁移。

### 实际验证

| 命令 | 结果 |
| --- | --- |
| `npm test` | 通过：生成契约检查、新旧生产 TS/Vite、新 unit/runtime 测试 TS、21 个 unit/hook、全量 Go、33 个 Playwright（27 旧 UI + 5 新 UI + 1 新 runtime 原生视频；浏览器 2.0m） |
| `npx playwright test tests/runtime/catalog-ui.spec.ts --repeat-each=3` | 15/15 通过，25.8s；每轮独立临时目录 |
| `go test -race ./...` | 全量通过，无 race；旧 app 433.270s、playback 3.473s、HTTP 29.483s（部分未改包使用缓存） |
| `go vet ./...` / `go build ./...` / `GOOS=linux GOARCH=amd64 go build ./...` | 通过；Linux 仅交叉编译 |
| `npm run typecheck:tests` / `npm run test:unit` | 通过；最终全量命令再次执行 |
| `npm run baseline:check` / `npm run format:check` / `git diff --check` | 通过；97 个旧基线声明保留 |

全量命令与 Go race 部分并发；时长是测试总时长，不是性能 SLA。新 UI 浏览器实测覆盖空库首页、只读详情、50→3 条分页、分组 10 条独立翻页、前进后退/刷新、搜索/排序/列表 URL、真实收藏增删、真实扫描导致单库/子组 cursor 失效恢复、季/单集分页、真实设置 CAS、401 返回原 URL、390px 无横向溢出及 Enter/Escape 导航。桌面和 390px 截图已查看，保存在测试输出而非提交媒体文件。

开发中修复无错误状态误显示错误、DOMException 取消识别、select 可访问名称；测试准备按真实后端的无来源任务状态、去后缀标题与 macOS 规范路径调整，不改变后端语义来迁就预期。所有扫描/收藏/设置/登录都作用于临时 fixture，没有用户数据操作。

### 仍未完成，不勾选整票复合验收项

1. 新建库/媒体库管理/扫描 UI、来源顺序与扫描状态、显式资料刷新及失败目标分页；浏览器中的建库仅通过真实 API 准备 fixture。
2. 手动匹配候选/选择和前端 CAS 竞态、人物/资料/图片完整新 UI 联动。
3. 新产品播放器状态机、单在途+最新待写快照、三秒关闭保存、字幕 Blob/切换、外部协议/复制兜底、删除预览/确认及部分成功/结果未知交互。
4. 全部旧视觉与键盘交互对照、完整新 UI 全链路、正式装配切换；当前基础 CSS/页面不能当成全部旧 UI 等价替换。
5. 06 清理仍等待 05 完整验收。已给出旧手写路由/请求、全库列表、事件/轮询、全局 CSS 及临时双构建入口的清理线索，但它们仍有正式消费者，不提前删除。

未改 CONTEXT/ADR/schema/生产 HTTP；未部署、重启服务、迁移或清空现有数据。真实来源/海报、外部播放器、NAS、容器和跨设备仍未验证。
