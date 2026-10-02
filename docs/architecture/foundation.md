# 重构阶段 01：契约、存储与验证基线

日期：2026-10-02。对应 `.scratch/architecture-refactor/issues/01-contract-schema-baseline.md`。

**这是后续 module 的实现基础，不是已切换的产品版本。** `cmd/reelnest` 仍装配旧 `internal/app`；当前运行契约仍见 `docs/api.md`。未挂载新路由、不迁移已有数据、不更换 DATA_DIR、不重启服务。任务 02–05 逐步接入，06 删除旧路径后才整体交付。

## 1. 唯一契约与检查入口

- `api/openapi.yaml`：新契约唯一来源，39 个 operation，覆盖 R01–R12；包含 JSON DTO、分页/TTL、错误码、凭据隔离、二次删除与二进制/Range/HEAD。
- `web/src/generated/api.d.ts`：只生成不手写。UI 展示类型应在自身 feature 定义，不能改此文件或恢复旧 `sourceId/tmdbId`。
- `scripts/contracts.mjs`：锁定 openapi-typescript 与 Prettier，内存生成后精确比较；检查不改文件。
- `api.Contract`：加载/验证 OpenAPI，严格检查请求/响应形状、16 KiB 请求体、未知/重复 query、尾随 JSON；恢复请求体供显式 DTO 解码。**它不是鉴权 middleware**，不会验证 Cookie、Origin 或串流 HMAC；02/05 的真实鉴权测试不得用它替代。
- `api/testdata/examples.json`：逐 operation 的受审样本。契约测试对所有 JSON/binary/HEAD/Range/错误响应执行校验；新增 operation 必须补样本。覆盖未知字段、字符串 ID、nullable 时间、非法枚举/页大小、响应漂移和不可重试的删除部分成功。
- OpenAPI 负责形状约束；跨字段/业务约束（例如进度 position ≤ duration、来源字段适用性、Token 换址保护、CAS、路径范围、真实授权）由拥有该规则的 module 实现，不能因 shape check 通过而跳过。

```sh
npm run contracts:generate
npm run contracts:check
npm run baseline:check
npm run build
```

生成检查已经接入 `build`，因此 `npm test` 也检查契约漂移。全量验证脚本 `npm run verify:foundation` 还检查旧测试清单、Go race/vet、浏览器及格式。

### 固定的补充分页约定

方案要求列表有界，因此季摘要、字幕列表也有独立的 `page`，不存在静默截断。单集页以**文件版本**为一行（episodeNumber + MediaCard），按集号/文件 ID 排序，避免一集有大量版本时嵌套无界数组。MediaDetail 不带全季集，SeriesDetail 只带一页季摘要。`GET /readyz` 与 `/healthz` 分别表达 DB/worker 就绪和存活。

## 2. Store 的 interface 与不变量

`store.Open(ctx, dataDir)` / `Close()` / `Keys()` 是当前最小 interface。writer/reader 与事务不导出；后续添加 `CommitScan/CommitMatch/MarkFileDeleted` 等用例级操作，而不是导出任意 SQL 或每表 Repository。

- 一份 `internal/store/schema.sql`，SQLite STRICT 表、真实 FK/CHECK、相关唯一索引与少量不变量触发器。没有 ALTER/历史表猜测/兼容回填。
- 一条写连接、最多四条只读连接；**每条连接**配置 foreign_keys=1、busy_timeout=5000。读连接同时 mode=ro + query_only，应用库使用 WAL。
- 新目录只允许为空；应用标识 RNST、user_version=1、schema_info 的 DDL 摘要必须吻合。非空无关/旧目录、不支持版本、缺失/损坏/替换密钥一律拒绝；不创建替代密钥，不 chmod/擦除旧状态。
- 新初始化生成独立 32 字节 `stream.key` 与 `metadata-settings.key`，均 0600；DB 记录密钥摘要，重启校验完整性。`Keys()` 返回值副本，仅用于装配串流及 AES-GCM module，不可记录/序列化。账号初始化、凭据加解密及 registry 发布在 02/03 实现。
- DATA_DIR 目录句柄持有 advisory flock，关闭两个池后才释放；目录本身锁定不产生额外 lock 文件。拒绝最终路径为符号链接。仅承诺 Linux/macOS 服务端；不宣称防御能够任意改应用目录的宿主机攻击者。
- SQLite **mode=ro 仍可能创建 WAL/SHM**：正常干净库用 immutable 只读检查；有未 checkpoint WAL 时，先复制 DB+WAL 到私有临时目录检查，再打开原库。这样不漏掉 WAL 里的版本/提交，也不改被拒绝目录。代价是崩溃重启时的额外磁盘/IO；临时目录自动清理，临时副本同样是敏感数据。普通干净重启不复制整库。
- 初次初始化失败/崩溃留下的非空未完成目录也拒绝，不猜测恢复；操作者须保留现场并显式选新空目录。正常新版本重启保留状态，不重新生成账号/扫描任务；业务 worker 恢复在 02/03 接入。

### 关系与事务交接

| 关系 | 已有数据库约束 | 后续提交者必须保证 |
| --- | --- | --- |
| 文件/电视剧/季集 | 同库组合 FK；每文件最多一个位置；多版本独立 | 完整扫描发布、空系列清理、指纹/根身份验证 |
| 资料目标/选择 | 文件或系列二选一、类型匹配、同库 FK；已组织文件禁止另建目标 | 组织前事务性删除原文件 subject；不扩散文件手动选择 |
| 外部身份/快照 | catalog+namespace+kind+原样字符串唯一；目录空间与外部身份不可原地改写；绑定指向身份而不是内容 | 地址规范化、无 DB 搜索、过期显式过滤、条件提交 |
| 人物/演职员/图片 | 当前快照 FK；人物同 catalog；演员/导演独立；photo 必须关联本快照 credit；图片≤5MiB | 无 ID 人物的作品限定 key、受控图片引用/MIME/内容签名、TTL 不超快照 |
| 任务 | subject 级联、generation/attempt 范围、provider revision 独立关系 | 成功/失败/补充结果均核对全部 revision；旧任务不清新 generation |
| 投影/家庭状态 | 文件/系列真实 FK、subject 删除置空、同库 subject；收藏/进度各自真实 FK | 同事务只更新受影响投影/revision；不复制收藏/进度真源 |

schema 测试使用真实 SQLite：拒绝非法关系/NaN/Inf/过大图片、事务失败回滚、目标级联、到期只删内容不删手动绑定、来源/地址/大小写隔离、四读连接约束、正常/真实进程崩溃重启、拒绝旧库前后字节摘要不变。

## 3. 新旧行为差异（不是删除产品能力）

| 需求 | 旧实现 | 新契约/交接 |
| --- | --- | --- |
| R01 | 分散初始化/迁移、环境资料配置导入 | 只认新空 DATA_DIR；会话与永久单文件串流仍隔离；家庭账号 `_FILE` 保留 |
| R02 | `/api/directories`，名称/来源各 POST | `/api/libraries`，PATCH revision+name/sources；目录/媒体库一行；同名仍隔离 |
| R03 | 轮询全目录列表取得 scanStatus | 独立 `POST .../scans` 和 `GET /api/scans/{id}`；queued/running/completed/failed/interrupted；失败不发布 |
| R04 | 系列详情嵌全部季集/文件；文件仍全量下发 | 独立系列、季摘要、单集文件版本分页；本地组织不依赖资料 |
| R05 | `/api/library` 全量，前端过滤/分组/排序 | `/api/home`、`/api/catalog/groups`、`/api/catalog`；全局分组摘要+组内 cursor，单库独立页 |
| R05 | 搜索后的系列计数可能缩小 | 单集名命中完整系列，计数属于完整作品；仍是子串，不换 FTS |
| R05/R06 | 最近添加前端聚合 | 全局最近 20 项，不按库提前截断；完整最近页不分组，时间相同按稳定 key |
| R06 | 收藏/进度 POST，未发生日期用 0 | 幂等 PUT；毫秒/null；文件与系列收藏独立；只有 continue/favorites cursor 依赖其 revision |
| R07 | matching kind+target、整数 sourceId/tmdbId | 显式 subject，typed RemoteRef（catalog、namespace、原始 remoteId、providerRevision），expectedRevision+subjectRevision |
| R07 | matched 冗余布尔、选择/缓存混在 payload | selection 与 cacheState/attemptState 独立；到期 metadata=null，不丢手动选择 |
| R07 | 全局资料锁，设置期间“忙” | 任务 generation + subject/binding/policy/provider 版本条件提交；无锁网络 I/O |
| R08 | poster/artwork/person-photo 多路由、旧 poster alias | `/api/assets/{id}/content`；引用与有效快照/人物同事务；卡片不带全演职员/BLOB |
| R08 | 来源人物 key 路由/缓存补丁 | `/api/people/{id}` + 有界 works；只查当前有效本地关联，不按同名合并 |
| R09 | settings POST，首次自动导入旧环境 | PATCH revision；不再 legacy 导入，空输入/清除/换址 Token 安全/加密持久化不变 |
| R10 | 响应/进度字段直接来自 app 类型 | 显式 Playback/Progress DTO；null 表达未保存；Range/If-Range/实际 ended/同句柄读取不变 |
| R11 | 永久单文件链接、SRT/VTT | 权限与读取语义保留；字幕列表分页/内容独立，字符串 ID；IINA/PotPlayer/VLC/复制在05接入 |
| R12 | delete 独立二次确认，多种部分成功返回 | DeleteResult 固定 fileOutcome+indexOutcome+retryable=false；可能 unlink 后不返回普通可重试错误 |
| 全部 | `{error:中文}`、部分 Unix 秒/0 | `{error:{code,message,retryable,requestId}}`、字符串 ID、毫秒/null、默认16KiB严格 JSON |

## 4. 回归保留与删除边界

`regression-inventory.json` 逐项记录 **97 个声明**（Go 测试/1 个 TestMain harness、Playwright 静态/参数化声明）。浏览器实跑为 27 个场景，不用声明数冒充实际场景数。

- `rewrite-contract`：按新契约改写，真实文件/HTTP/SQLite/浏览器语义全部保留。
- `retain-harness`：真实进程崩溃 harness 保留。
- `split-preserve`：只移除旧 JSON/迁移/粗锁部分，保留正常重启、手动选择、退避/重试、凭据安全及并发一致性。
- `remove-legacy-only`：**仅列未来删除资格，本阶段一个旧测试也未删除。** 四个用例是旧资料环境导入、旧目录名回填、旧日期回填、旧 TMDB 数据迁移。不得把新库正常重启、家庭账号 `_FILE`、有效资料补充刷新误判成兼容。

`baseline:check` 现在要求所有清单项仍存在；未来 02–06 迁移测试时须连同可执行替代证据更新清单/检查规则，不能删除检查器来绕过。R01–R12 均有回归组追踪；真正已切换与未切换的 coverage 在各 issue Answer 分开记录。

## 5. 可复用样本与测量

```sh
go test ./internal/store -run TestSynthetic -count=1 \
  -args -baseline-output="$PWD/docs/architecture/schema-baseline.json"
```

`fixture_test.go` 的 `populateSynthetic` 为后续 store 查询测试提供共用样本：10 库，1,000/10,000/50,000 文件；目标数分别 1,000/5,000/5,000；每目标100演员+50导演、结构化快照/图片引用、少量收藏/进度。50,000 文件用5,000电视剧的独立单集文件版本实现，目标均有真实本地关系；没有创建/读取真实媒体。

本次测量机器为 Apple M5 Pro / 18 CPU / 48 GiB、macOS arm64、Go 1.25.12、Node 26.7.0；测量期间同时运行全量 Go race 回归，并非空闲专用基准机。

报告包含环境、每规模实际样本数、生成耗时、四类 SQL、EXPLAIN、返回行数、序列化行 JSON 大小、首次查询及30次热查询 p50/p95。**这不是生产 HTTP 基准**：行 JSON 不是最终 DTO，返回行数不是 SQLite 实际访问行数，首次读取也不是清空 OS 页缓存的冷 IO。04 必须接入真正首页/详情/人物/分页查询并追加 SQL 次数、查询工作量、响应体、冷热条件及增长曲线；本阶段不能宣称200ms/256KiB等最终目标已达标。

## 6. 未接入/未验证

- 02 的身份/mediafs/扫描事务与任务恢复，03 的资料提交/加密/图片并发，04 的生产查询与性能，05 的新前端/播放/删除，06 的旧实现清理仍未完成。
- Store 实测在 macOS arm64；Linux/amd64 仅交叉编译，不等于 Linux 文件锁/容器实跑。
- Docker、真实 TMDB/TVDB/MetaTube、NAS、外部播放器实机及其他编码不由 fixture 证明；没有部署授权或服务切换。
- 测试结果与具体命令见对应 issue Answer。本基础不修改旧 ADR、领域词汇或当前使用文档。
