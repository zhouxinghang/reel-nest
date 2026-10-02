# 重构阶段 02：安全文件、身份与媒体编目

日期：2026-10-02。对应 `.scratch/architecture-refactor/issues/02-mediafs-identity-catalog.md`。

**本阶段是新运行路径的后端基础，不是生产切换。** `cmd/reelnest` 仍使用旧 `app.New` 和旧前端。新的 `app.NewRuntime` 只装配本阶段已实现的身份、媒体库、扫描 HTTP interface；文件/电视剧的最终 HTTP 读投影属于 04，播放/字幕/删除的授权与确认交互属于 05，命令入口切换与旧路径删除属于 05/06。没有迁移、清空、导入现有数据，没有读取实际凭据或重启服务。

## 1. Module 与 interface

| Module | Interface / 职责 | 不对调用者泄露的实现 |
| --- | --- | --- |
| `mediafs` | `Inspect`、`Walk`、`OpenVideo`、`Sidecars`、`OpenSubtitle`、`ObserveDeletion`、`Unlink` | 根范围/应用数据隔离、逐级 `openat(O_NOFOLLOW)`、regular-file 核对、指纹、父目录身份、ctime、精确 `unlinkat` |
| `identity` | 初始化一次、`Login`、`Session`、`Logout` | bcrypt、哈希会话、过期/轮换、一个密码校验槽和 5 分钟/10 次失败限流；不认识串流凭证 |
| `catalog` | 建库/修改/扫描、安全打开、`WithMutation`、`Close` | 两个扫描 worker、持久队列、每库变更预留、生命周期合并、保守季集识别 |
| `store` | 媒体库配置、扫描 staging/发布、家庭状态、定向文件读取 | 一写四读、SQLite 事务、依赖清理、资料目标与派生投影；无导出的 SQL/Tx |
| `transport/http` | 身份、媒体库、扫描、healthz/readyz 路由 | 同源校验、显式 DTO、OpenAPI 请求验证、错误脱敏、配置列表游标与 Cookie |
| `app.NewRuntime` | 新 schema 的装配、Handler、Close | 初始化失败清理、先停止 worker/变更再关闭 DB/释放 DATA_DIR 锁；无旧库兜底 |

`model` 只含共享纯值/错误，没有 I/O；`catalog.IdentifyEpisode` 是纯函数，沿用保守识别规则，不根据线上资料改写本地组织。

### 文件能力与授权分工

- `Root` / `Ref` 是**已存储身份的描述值**，不是授权。真正读取返回已核对且可关闭的 `*os.File`，调用者不得自行 `os.Open(filepath.Join(...))`。
- `mediafs` 不接收 HTTP 请求或数据库句柄。它不能授予家庭账号、串流或删除权限；这些由 transport/playback 的调用路径负责。
- `Observation` 是删除确认材料，不是授权凭据。05 必须对文件 ID、绝对路径、父目录身份、含 ctime 的版本和到期时间做域隔离签名，提交时重新验签/检查到期/二次确认并取得 `WithMutation`。
- `Unlink` 已区分 `deleted` / `not_deleted` / `unknown`。返回 deleted 或 unknown 后不能自动重试；05 必须用独立有界上下文做索引核对，输出契约的 `fileOutcome` / `indexOutcome`。本阶段没有开放删除 HTTP 路由。
- `Sidecars` 只枚举已核对视频的同目录同 stem 字幕，分批返回；打开时复核视频和字幕版本。UTF-8/SRT/VTT 转换、2 MiB 内容上限、分页游标/字幕身份签名由 05 集成。字幕绝不经 `Unlink` 删除。
- 相对路径越界、符号链接、FIFO 等非普通文件不能读成视频；目录枚举不跟随符号链接。可信部署路径的 `/var` 等别名只在配置时规范化，缺失根也解析其既存父路径，防止别名绕过 DATA_DIR 重叠检查。
- 文件身份仍是设备/inode/大小/mtime，不是全视频内容哈希；删除确认额外使用 mode/ctime。POSIX 不支持 atomic unlink-if-inode，宿主机/NAS 外部写入必须避开最终复核/unlink；已有打开句柄可能继续读取。没有增强为跨文件系统/数据库原子事务。

## 2. 扫描与持久化不变量

1. 媒体库与首个 queued 扫描在同一事务创建。同名库仍独立，路径/祖先/后代/根身份重叠拒绝；改名不改磁盘。空资料源表示禁用自动获取，省略则保留 `[tmdb,tvdb]` 默认。
2. 两个固定 worker 原子领取 `scan_runs`。同库 queued/running 扫描预留变更权；删除/另一扫描立即得到 `LIBRARY_BUSY`，而不是等待全库遍历。不同库可同时扫描，读取/进度不取得变更锁。
3. `Walk` 每次 `ReadDir(256)`，每批最多 256 文件写 `scan_entries` 和新增 `scan_organization`。目录递归深度上限 256；超过上限使整个扫描失败，不静默跳过。内存不持有整个文件清单。
4. 任意读取/目录变化/取消/暂存失败均不发布。访问期间逐级句柄核对，遍历完成重新核对各目录及根；文件系统并非原子快照，外部程序同时增删仍须由后续手动扫描对账。
5. `CommitScan` 以集合式 SQL 消费 staging（不把清单读入 Go 内存、不逐文件执行长串 SQL），一个事务合并文件版本、组织季集、维护资料目标、移除缺失文件及依赖/空电视剧、更新本库投影、排队新资料目标、递增 catalog revision、标记 completed 并删除 staging。
6. 同路径同版本复用 ID、added_at、进度、收藏和适用的资料目标。新版本获得新 ID，旧版本及家庭状态被级联清理。未组织文件转为系列单集时，先删文件 subject/选择，再建立组织关系；不能把文件手动选择传播到整部电视剧。
7. 资料策略 revision 与普通配置 revision 分开。来源策略变更只给自动选择目标递增任务 generation，保留手动选择。03 仍须实现完整任务版本快照/成功失败条件提交，不能只依赖本阶段外键和 generation。
8. `MarkFileDeleted` 保留 deleted 记录直到成功扫描清理，不直接移出媒体库；读取凭真实文件身份拒绝已缺失/替换的对象。进度/收藏使用真实外键，写入与扫描清理经同一写连接的短事务串行。
9. 停止接受任务、取消扫描、等待 worker 和已开始的变更（含 unlink 后核对）、清理未完成任务、最后关闭数据库。任务登记与 closed 标志受同一生命周期锁保护，不会在 Wait 后 Add。
10. 新启动将遗留 queued/running 标记 interrupted、清除 staging；不自动扫描、不重建账号、不替换密钥。沿用 01 的进程目录锁与严格 schema/key 检查。

新增 `SCAN_FAILED` / `SCAN_INTERRUPTED` 错误码同步写入 OpenAPI 和生成类型。schema 增加组织 staging 表，仍是未发布的新 schema；DDL hash 不兼容旧阶段的临时 fixture DB，不提供转换或运行时迁移。

## 3. 查询与后续接入

- `GET/POST /api/libraries`、`PATCH /api/libraries/{id}`、`POST .../scans`、`GET /api/scans/{id}` 已有真实 HTTP/契约验证；会话与跨站保护覆盖这些写入。
- 媒体库配置列表按 ID 做有界 keyset 分页，游标绑定 limit、catalog revision、最多 5 分钟有效期和独立签名域，拒绝修改/过期/版本不匹配；包含空库。它不是 04 的内容搜索/人物分页实现。
- `store.MediaFile` / `MediaFiles` 提供定向文件及所属季集/subject 的内部读取 interface，供本阶段集成测试和 03/05 接入。`MediaFiles` 每页最大 256，不是最终用户目录列表接口。
- 本地 `catalog_entries` 在发布事务内重建**受影响媒体库**的投影。标题排序/搜索目前只提供本地基础值；中文/数字友好排序、有效资料、全局分组、生产查询计数与 HTTP 性能验收留给 04，不据此声称全量查询优化完成。
- 身份 HTTP 已设置 HttpOnly、SameSite=Strict 和可配置 Secure，登录轮换旧会话，注销撤销服务端会话。只有 `identity.Session` 能授予管理权；串流密钥或任意 64 位凭证不等于有效会话。
- 启动配置 `_FILE` 读取仍由命令层承担；没有新增资料源环境兼容导入。06 切换命令装配时必须保留家庭账号 `_FILE` 支持。

## 4. 验证入口与回归追踪

所有测试只使用临时媒体/真实 SQLite；没有 mock 掉扫描、数据库提交、权限或 unlink 来证明成功。

| 范围 | 主要测试位置 | 追踪 |
| --- | --- | --- |
| 目录/文件/符号链接/替换/特殊文件/取消/精确删除 | `internal/mediafs/mediafs_test.go` | R01–R04、R11–R12 |
| staging、完整发布/真实 SQL 触发器失败回滚、目标变化、状态保留与级联 | `internal/store/scans_test.go` | R02–R04、R06–R07、R12 |
| 保守文件名规则、多版本、纯数字/季目录 | `internal/catalog/organization_test.go`、store 测试 | R04 |
| 跨库并发、12,000 新文件的取消/旧索引保留、读失败、关闭与任务登记 | `internal/catalog/catalog_test.go` | R03、R06、R12 |
| 真实子进程不 Close 退出后的 WAL/会话/进度/扫描恢复、进程锁 | `internal/catalog/catalog_test.go`、01 store 测试 | R01、R03、R06 |
| 账号初始化/轮换/注销/到期/限流、不接受伪造会话 | `internal/identity/identity_test.go` | R01 |
| 登录→建库→扫描→查询组织→安全打开、全部实际响应的 OpenAPI 校验 | `internal/transport/http/http_test.go` | R01–R04 |
| 新装配关闭/重开、会话与密钥保持、单 DATA_DIR 独占 | `internal/integration/runtime_test.go` | R01 |

新增 23 个顶层测试声明（其中一个仅作子进程 helper）；旧 97 个基线声明完全保留，未以“兼容清理”为由删除旧安全测试。03–06 仍需补自己的替代证据才能删除旧路径。

```sh
npm run contracts:check
npm run baseline:check
go test -race ./internal/mediafs ./internal/catalog ./internal/identity ./internal/store ./internal/transport/http ./internal/integration -count=1
go test ./internal/store -run '^TestLargeScanUsesBoundedStagingPages$' -count=1 -v -args -scan-fixture-files=50000
npm test
go test -race -count=1 ./...
go vet ./...
GOOS=linux GOARCH=amd64 go build ./...
npm run format:check
git diff --check
```

50,000 文件命令是 **SQLite staging/发布测试**，不是生产 HTTP 基准或 50,000 实际媒体文件的扫描测量。本机最终一次暂存约 0.722 秒、集合式发布约 1.851 秒（与其他回归并行）；这是单次 SQL 工作负载结果，不是延迟 SLA。实际完整命令结果及未验证项记录于 02 的 Answer。默认测试用 1,000 行，避免每次回归重复大样本成本；真实文件扫描并发/中断测试额外使用 12,000 个新增文件。发布事务只用临时表捕获新增资料目标，从而既能集合式写入，又不会因重扫重新排队未变目标。

## 5. 未验证与交接限制

- 03 的真实资料 worker/provider/图片逻辑，04 的完整查询与性能，05 的网页/播放器/删除确认，06 的单一路径清理尚未完成。本阶段不把旧 UI 的 27 个浏览器回归解释为新 UI 验收。
- 实测是 macOS arm64；Linux/amd64 交叉编译不等于 Linux/容器运行。Docker CLI 缺失；真实 NAS、只读挂载 EROFS、异常文件系统 unknown unlink、三类资料源、IINA/VLC/PotPlayer、其他音视频编码未验证。
- `Close` 等待已开始的变更回调；后续 playback 必须确保 unlink 后的数据库核对有界，不能在回调中挂无限期网络工作。受阻的底层 NAS 系统调用不能被 Go context 强行打断。
- 初次账号/应用目录已初始化后，重启不得因部署输入变化重置账号。旧数据库/缺失密钥/旧 DDL 不兼容须继续拒绝，不新增“修复”性重建。
