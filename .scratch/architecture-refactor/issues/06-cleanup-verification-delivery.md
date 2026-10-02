# 06：兼容清理、完整验证与交付文档

Status: resolved
Blocked by: 02, 03, 04, 05

Parent: [全量重构技术方案](../spec.md)

## 目标

收敛到一个可运行、可验证、无历史兼容分支的实现，完成完整回归和当前态文档；不把代码交付自动扩大成真实环境部署。

## 范围

- 按 spec 第 12.1 节逐项删除旧 app 业务实现、旧字段/别名、迁移初始化、整数 MetaTube 映射、旧资料源环境导入、旧 Compose 与升级脚本。
- 清除前端旧广播、re-export、未使用卡片、重复 CSS 和仅历史兼容测试；保留文件安全、有效刷新、家庭账号 _FILE、只读/读写挂载分工。
- 检查 module 依赖方向，App 仅装配，provider 无 SQL，handler 不承载文件安全或业务事务。
- 将 README 与 docs/api.md 重写为已实现当前态；历史 issue 和旧验证记录保留，新增本次验证记录及切换/回退步骤。
- 完成最终自动化与性能验证，分别记录真实服务/容器/播放器的已测和未测情况。

## Acceptance criteria

- [x] 新前后端只有一套运行路径；没有把旧单体改名保留、兼容双写或服务端旧契约兜底。
- [x] 所有删除项通过引用和回归核验；纯迁移代码已删除，新版本正常重启及状态持久化仍成立。
- [x] 契约生成检查、前端 build/unit、Go test/race/vet、Playwright、格式和 diff 检查通过并留结果。
- [x] spec 第 11 节性能门槛有可复跑数据；未达标项明确记录且不把未完成任务标记 resolved。
- [x] Docker 可用时验证非 root、只读/读写挂载和持久化；不可用时明确列为未验证，不以配置文件存在代替实测。
- [x] 安全说明保留永久删除不可恢复、外部文件系统竞态、长期串流凭证风险及真实播放器兼容性边界。
- [x] 新空 DATA_DIR、不导入旧状态、拒绝旧 schema、不自动清空现有数据和备份/回退说明完整。
- [x] 无真实凭据/媒体操作、无自动服务重启或部署；如用户另行授权部署，单独记录该动作与验证。
- [x] 六个 issue 的 Answer、状态、地图和 Frontier 与实际完成情况一致。

## 验证与交付

完整执行本任务列出的门禁，并对照 R01–R12 逐项说明交付证据。最终回答区分代码完成、测试通过与部署状态；不能用“已通过评审”代替任何实现或验收证据。

## Comments

- 2026-10-02：05 前端基础增量交接见 [frontend-foundation](../../../docs/architecture/frontend-foundation.md)。新增可核对清理线索：旧手写路由/请求、全库列表、metadata-updated/重复轮询、全局 CSS，以及阶段性 runtime HTML/双构建入口。它们现在仍有正式或测试消费者；本票仍受 05 阻塞，不认领、不提前删除旧安全回归或切换生产数据。

- 2026-10-02：05 已 resolved，所有前置依赖已满足，本票仍保持 ready-for-agent、尚未认领。当前正式源码已为新 runtime + 新 UI；新 [完整交接](../../../docs/architecture/frontend-interactions.md) 含逐项消费者/替代证据。重点清理 `tests/legacy-server`/旧 `tests/server`、`vite.legacy.config.ts`/多构建、无引用的 `runtime.html`、旧 app 业务和旧前端；不要直接删除 97 个基线中的安全/正常重启证据。正式 CLI 的 `_FILE`、重启和旧目录拒绝已有新浏览器测试；保留新的本地 provider fixture。README/API 现已加切换警告，但完整当前态改写和最终性能复测仍属本票。05 门禁：37 unit/hook、39 浏览器、全量 Go race、新 runtime 三轮 36/36，详见 05 Answer；未部署或操作真实数据。

- 2026-10-02：认领 06；工作区干净，01–05 均 resolved。按 spec §12.1 清理旧路径，逐项追踪原 97 项回归的保留/替代证据，执行最终功能与性能门禁；不部署、不操作现有 DATA_DIR 或用户媒体。


## Answer

2026-10-02：06 完成，代码与隔离验证交付，**没有部署或操作现有数据**。完整清理审计、R01–R12 证据、性能和未验证边界见 [最终交付记录](../../../docs/architecture/final-delivery.md)。

### 改动

- 删除旧 `internal/app` 业务/SQL/provider/迁移及原测试，App 仅装配。移除 optionalSecret、旧资料源导入/字段/路由兜底/整数 MetaTube 桥接/纯升级脚本与三个旧资料源 Compose；保留家庭密码 `_FILE`、安全文件句柄、正常重启、有效资料刷新/失败重试以及媒体挂载只读/读写分工。
- 删除旧顶层前端、广播、re-export、卡片/CSS 和双测试构建。一次生产前端构建，正式 CLI 与测试 runtime 使用同一嵌入资源；无旧运行栈消费者。新增模块依赖/SQL归属/旧符号回流检查。
- 原 97 项审计记录名称/需求/分类/备注保持不变，4 个纯迁移项退役，93 项关联到新契约可执行测试。混合升级项保留正常持久化/刷新/安全语义；真实文件/SQL失败与UI提示分层的迁移边界逐项说明，没有删除全部历史测试后只靠少量冒烟替代。
- 新增 4 个 TMDB/TVDB/故障浏览器流程、旧契约拒绝、20 秒慢源 HTTP 基准和 50k 实际文件遍历。资料 HTTP origin 是唯一替换点，生产默认client不变；浏览器fixture拒绝其他出站origin，测试启动要求OS临时目录下的新空非符号链接目录。
- 修正 Dockerfile 漏拷贝后端 `api/` 依赖；没有把配置修复等同于容器实测。README/API 重写为当前态，明确新空 DATA_DIR、旧状态不导入、拒绝旧schema、敏感备份/切换/回退和永久删除风险；历史issue/验证记录保留。

### 实际验证

- `npm test`：生成契约、生产/测试 TypeScript、Vite、**37 unit/hook**、全量 Go、**16 Playwright** 全部通过，最终浏览器一轮43.4秒。
- `go test -count=1 ./...`、`go test -race -count=1 ./...`、`go vet ./...` 通过；全量race最终store约22.37秒、HTTP约30秒，无race报告。生产Go构建、Linux amd64 CGO=0交叉编译通过；`go mod tidy -diff`无差异。
- `npx playwright test tests/runtime --repeat-each=3`：**48/48**，约2分钟。测试根目录保护补充后，正式CLI浏览器冒烟再次通过；非空测试根目录被拒绝且sentinel未变。
- `npm run baseline:check`、`npm run architecture:check`、`npm run format:check`、gofmt、`git diff --check` 通过；原97条记录内容与起点逐项核对一致。目视检查桌面媒体页、390px删除确认及TVDB电视剧页。
- `go test ./internal/store -run '^TestCatalogHTTPPerformance$' -count=1 -timeout 15m -v -args -catalog-http-report=…`：三档9端点、各30热样本通过。50k档：首页p95 **86.301ms**，50条列表 **7.193ms**，详情 **0.976ms**，人物50部作品 **16.440ms**；最大相关JSON54,471B，详情7条store SELECT+1条鉴权，无无关演职员解析。原始 [JSON](../../../docs/architecture/catalog-http-final.json) 包含环境/首读/SQL计划；不是OS冷盘或生产SLA。
- `go test ./internal/transport/http -run '^TestSlowProviderHTTPIsolation$' -count=1 -timeout 2m -v -args -slow-provider-report=…`：真实上游持续20.002秒，30次本地设置保存p95 **1.123ms**（≤200ms），首页3.850ms、进度1.595ms；[原始数据](../../../docs/architecture/slow-provider-final.json)。
- `REELNEST_LARGE_WALK=1 go test ./internal/mediafs -run '^TestLargeWalkRemainsBounded$' -count=1 -v`：真实50,000临时文件，196回调/最大256项、遍历22.818秒。`go test ./internal/store -run '^TestLargeScanUsesBoundedStagingPages$' -count=1 -v -args -scan-fixture-files=50000`：暂存655.121ms、发布2.978秒。取消/失败零发布、跨库并发与图片4/2上限由全量/race专项覆盖。

### 未验证与交付状态

Docker CLI不可用：容器非root、只读根/挂载和持久化**未实测**；真实三源、NAS/EROFS、跨设备LAN、IINA/PotPlayer/VLC实机、其他浏览器/音视频编码仍未验证。VP8 fixture无音轨。保留POSIX外部文件系统竞态、不可保证立即取消NAS调用、长期流凭证和关闭进度best-effort边界。

没有读取真实凭据、扫描/修改用户媒体、迁移/清空现有DATA_DIR、部署、重启服务或push。01–06均resolved，地图Frontier为空；部署/切换/回退仍需单独授权。
