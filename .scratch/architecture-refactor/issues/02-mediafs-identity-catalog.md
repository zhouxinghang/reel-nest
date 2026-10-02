# 02：文件安全、身份与媒体编目

Status: resolved
Blocked by: 01

Parent: [全量重构技术方案](../spec.md)

## 目标

建立有真实文件安全保障的 module seam，完成新建媒体库、身份会话、扫描、季集组织及持久化闭环，保留原实现的高价值安全行为。

## 范围

- 提取 `mediafs` 的根目录/逐级句柄访问、身份/指纹、视频/字幕安全打开及单文件 unlink 能力，业务调用者不能拿已校验路径再自行打开。
- 迁移家庭账号、会话、同源保护、登录限流，隔离会话和串流只读权限。
- 实现媒体库配置、名称、根范围/重叠检查、保守文件名识别和季集关系；本地组织不依赖线上资料。
- 实现 scan_runs/staging、有界 worker、每库变更互斥和完整发布事务；跨库不共享遍历锁。
- 落实 DATA_DIR 单进程锁、任务领取/停止/关闭顺序，保留家庭状态相关持久化基础。

## Acceptance criteria

- [x] 真实临时媒体完成登录→建库→首次扫描→文件/季集查询→安全打开；未配置任何资料源也可完成。
- [x] 名称校验、同名库隔离、目录重叠/越界/符号链接、根替换、特殊文件与应用数据重叠均正确处理。
- [x] 同路径同版本重扫不改变 ID、收录时间、进度或收藏；新版本不继承旧文件身份。
- [x] 完整成功扫描才移除缺失文件及依赖/空电视剧；任何中断/部分读取失败不发布部分结果。
- [x] 文件清单与目录读取分批、有界；同库任务互斥、跨库可并行；进度/读取不等待整个扫描锁。
- [x] 扫描、组织和资料目标变更的原子性及失败回滚通过真实 SQLite 测试。
- [x] S01E01、作品目录内纯数字、季目录、多版本及保守未组织规则通过回归。
- [x] 重启正确处理 interrupted 扫描；不自动扫描、不重复初始化账号、不让两个进程使用同一 DATA_DIR。
- [x] 为任务 03、05 提供可测试的安全读取/删除能力，原有路径安全测试覆盖保留。

## 验证与交接

使用真实临时文件/SQLite、进程重启/中断及 race 测试。删除相关底层测试仅操作临时视频；HTTP 二次确认与前端交互由任务 05 集成，不在此任务省略其安全要求。

## Comments

- 2026-10-02：认领 02；基线 `566bb79`，工作区干净。按已通过方案建设新 mediafs/identity/catalog/store 闭环，保留旧运行路径和回归直到 05/06 切换清理；仅使用隔离临时媒体/数据库，不读取凭据、不操作现有数据或服务。

## Answer

2026-10-02：本任务完成，实现及验证记录如下。本阶段仅交付新后端基础，不切换 `cmd/reelnest`/旧前端，不部署、不迁移用户数据。交接详见 [阶段 02 文档](../../../docs/architecture/catalog-foundation.md)。

### 交付

1. 新 `mediafs` 以逐级 `openat`、`O_NOFOLLOW`/`O_NONBLOCK`、根/文件/父目录身份和含 ctime 的删除复核提供已验证句柄；视频、同 stem 字幕、分批遍历及单视频 `unlinkat` 共用该 module。保留 not_deleted/deleted/unknown 风险分工，未开放删除 HTTP 接口。
2. 新 `identity` 提供一次性家庭账号初始化、bcrypt、哈希会话、30 天有效期、Cookie 轮换/注销、单校验槽与限流。HTTP 同源保护、HttpOnly/SameSite/Secure、请求契约验证及会话/串流权限隔离已接入身份/媒体库/扫描路由。
3. 新 `catalog` 与 store 用例方法实现媒体库创建/改名/资料策略 revision、规范根路径与重叠检查、初次扫描原子排队、保守文件名识别和独立文件版本。文件/季集/subject 可定向查询，无资料源仍可安全打开视频。
4. `scan_runs` + 256 条文件/目录批次 + `scan_entries`/`scan_organization` + 两个固定 worker。`CommitScan` 使用集合式 SQL 完整发布，同事务维护身份、组织、目标/资料任务、缺失清理与本库投影；文件身份/added_at/家庭状态在同版本重扫中保持，旧文件匹配不会扩散到整部电视剧。
5. 每库变更预留使同库扫描/删除互斥、不同库并行；读取/进度不持扫描锁。关闭先禁止登记任务、取消扫描、等待 worker/变更及 unlink 后核对，再关闭数据库；进程重启清除未发布 staging 并标记 interrupted，不自动重扫或重置账号。
6. `app.NewRuntime` 仅装配新 modules；沿用 01 的 DATA_DIR 独占锁和严格 schema/key 检查。新媒体库列表提供有界、签名、限时、绑定 revision 的分页。最终内容查询/资料模块/播放器及旧代码清理由 03–06 接续。
7. 新增 23 个顶层测试声明（含一个子进程 helper），保留全部 97 个旧基线声明。实际 HTTP 响应校验新 OpenAPI，新增 `SCAN_FAILED`/`SCAN_INTERRUPTED` 并同步生成 TS 类型。旧运行代码、既有 ADR、CONTEXT、生产 README/API 说明未改写。

### 实际验证

环境：macOS arm64，Go 1.25.12、Node 26.7.0、npm 11.19.0、本机 Chrome。仅真实临时文件/SQLite、隔离进程和受控旧上游 fixture；没有读取真实凭据/媒体或重启用户服务。

| 命令 / 场景 | 结果 |
| --- | --- |
| `npm run contracts:check`、`npm run baseline:check` | 通过；生成无漂移，97 个基线声明完整保留 |
| `go test -race ./internal/mediafs ./internal/catalog ./internal/identity ./internal/store ./internal/transport/http ./internal/integration -count=1` | 最终集合式发布代码通过；真实权限/替换/字幕/FIFO/unlink、事务回滚、会话、HTTP 契约、装配/重启验证通过 |
| `go test -race ./internal/mediafs ./internal/catalog -count=3` | 路径安全、12,000 新文件跨库扫描/取消、读失败与关闭并发重复三轮通过 |
| `go test -race ./internal/store ./internal/catalog ./internal/transport/http ./internal/integration -count=3` | 最终实现及补充回归重复三轮通过；含 staging 丢失拒绝发布、未变目标不重新排队 |
| `go test ./internal/store -run '^TestLargeScanUsesBoundedStagingPages$' -count=1 -v -args -scan-fixture-files=50000` | 通过；50,000 行分批 staging 约 0.722 秒，完整发布约 1.851 秒；零残留 staging |
| `npm test` | 通过；TypeScript/Vite、全量 Go、27 个旧 UI Playwright 场景（1.9 分钟）；旧 app Go 测试约 216 秒 |
| `go test -race -count=1 ./...` | 全量通过，无 race；旧 app 包 438.869 秒。其后发布优化及补充测试另经最终全部新 module race 和关键四包三轮 race 通过 |
| `go vet ./...`、`go build ./...`、`GOOS=linux GOARCH=amd64 go build ./...` | 最终源代码通过；交叉编译不等于 Linux 实跑 |
| `npm run format:check`、`gofmt -l`、`git diff --check` | 通过 / 无输出 |

50,000 行首次逐文件 SQL 版本的发布约 12.941 秒；改为集合式 SQL 后约 1.851 秒。两次均与其他回归并行，非空闲机器性能基准。这是 **SQLite 暂存/发布样本**，不是 50,000 实际媒体扫描、最终 HTTP 延迟或新前端性能验收。发布事务仍会短暂串行其他写入，不宣称扫描发布时进度写入零等待。

### 未验证及后续

- 03 仍须实现资料来源/图片与完整 revision 条件提交；04 仍须完成中文/数字排序、有效资料、分组/分页与最终性能；05 仍须将新文件能力接入播放、字幕、串流、删除二次确认及有界核对；06 才删除旧路径并交付切换文档。
- 新登录/建库/扫描由新 HTTP 实际验证；文件/季集查询与安全打开通过新 module interface 验证，最终详情 HTTP/新 UI 尚未交付。旧浏览器回归通过不等于新 UI 验收。
- Docker CLI 不可用；容器、真实 NAS/只读挂载 EROFS、文件系统 unknown unlink、三类真实资料源、外部播放器实机和其他编码未验证。单机 POSIX 外部写入竞态限制仍然存在。
- 没有实际切换用户 DATA_DIR、读取旧凭据、操作用户媒体、部署或重启服务。新 schema 的 DDL 摘要变化不兼容阶段 01 的临时 fixture 数据，无迁移/重建兜底。

- 最终状态：02 resolved；任务地图及父方案阶段记录同步，Frontier 为 03，未认领 03–06。
