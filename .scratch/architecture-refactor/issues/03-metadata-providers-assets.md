# 03：资料源、版本提交、人物与图片

Status: resolved
Blocked by: 01, 02

Parent: [全量重构技术方案](../spec.md)

## 目标

把三源协议、设置、选择、有效快照、人物与图片集中在资料 module 内，取消覆盖网络请求的全局锁，以受约束的版本提交保障正确性。

## 范围

- 实现结构化 provider interface，TMDB/TVDB/MetaTube adapters 不访问 DB、不生成本地路由、不为搜索写整数 ID 映射。
- 实现 provider catalog、原始字符串 ID/namespace、配置加密与 revision、不可变 registry 快照。
- 实现有界资料任务及 generation 合并，自动回退/手动选择和成功/失败/补充资料统一条件提交。
- 实现结构化演职员、本地人物作品关联、有效期及统一按 key 合并的图片缓存。
- 对旧目标、旧选择、旧策略、旧配置、旧目录空间的在途结果一律防止覆写。

## Acceptance criteria

- [x] 三源按目录优先级匹配；中文回退、候选完整性与唯一性、来源独立限流/冷却符合既有需求。
- [x] MetaTube 仅电影，provider/remote_id 保持真实身份，搜索不写库；同 ID 跨来源/服务地址不会混用。
- [x] 页面配置成功后即时生效；空输入保持/显式清除/旧 revision 冲突/换地址 Token 保护/密钥缺失拒绝启动均通过。
- [x] 慢上游不使设置保存等待网络；成功和失败提交均核对 subject、binding、policy、provider 和 job generation。
- [x] 目标被扫描移除、手动改绑、策略/配置变化后，旧任务不能复活目标、清除新任务或覆盖新选择；以可控交错测试验证。
- [x] 缓存到期不清除手动选择，不返回过期内容；人物只来自当前有效本地关联，不按同名猜身份。
- [x] 图片按 key 合并、全局/同源有界；命中不等慢图；取消不写负缓存；旧引用/旧配置结果不能污染新图。
- [x] 上游响应体、图片大小/类型、重定向、任意 URL 和凭据隔离保持安全；无凭据及上游失败不影响本地读取。
- [x] 不保留 payload 双真源、旧人物索引补丁或 legacy 配置迁入路径；有效刷新和失败重试能力保留。

## 验证与交接

使用真实 SQLite 与受控 HTTP 上游，覆盖三源 contract、race、配置/任务重启、图片并发及外键失败。向任务 04 提供有效快照/人物查询基础，向任务 05 提供已实现的资料 HTTP 契约。真实服务联通另列未验证。

## Comments

- 2026-10-02：认领 03；基线 `bbd6e62`，工作区干净，01–02 已 resolved。本轮先交付资料源设置、加密持久化、目录空间和不可变配置快照及新 HTTP 契约；资料任务 CAS、三源 adapter、人物和图片仍须分别完成后才可 resolved。仅使用隔离临时数据库/受控上游，不读取真实凭据、不切换或部署现有服务。

## Progress — 配置基础增量（2026-10-02）

**本轮只完成 03 的配置基础，整票未完成，Status 保持 claimed。** 复合验收项中的新页面、adapter、匹配提交、人物及图片尚未实现，故不勾选整项，不解除 04–06 的依赖。交接详见 [资料源设置与配置快照](../../../docs/architecture/provider-settings.md)。

### 已完成

- [x] 新 `metadata.Service`：AES-256-GCM、source/地址/revision 绑定、独立现有密钥、无 legacy 导入；不可变配置快照与短事务提交后原子发布。
- [x] 三源配置的保留/独立清除/字段及 UTF-8 字节限制、revision 冲突/no-op；MetaTube URL 规范化、目录空间隔离、Token 轮换及换地址保护；configured 不冒充联网验证。
- [x] 设置、目录空间、受影响任务 generation 与 catalog revision 的原子保存和失败回滚；保持旧空间手动绑定及其快照，旧任务 superseded，其他来源/已删除文件不重新排队。
- [x] 新版 GET/PATCH 设置 HTTP、家庭会话与同源保护、OpenAPI 请求/响应验证、媒体库资料源状态；新 runtime 装配/关闭/配置重启保留及缺密钥拒绝启动。
- [x] 新增 14 个顶层测试，扩展 runtime 重启测试；真实临时 SQLite、两类事务失败注入、24 路 CAS、取消/关闭、密文篡改、地址/凭据隔离与受控慢 HTTP 消费者验证。

### 实际验证

环境：macOS arm64，Go 1.25.12、Node 26.7.0、npm 11.19.0、本机 Chrome。只用临时数据与受控 fixture。

| 命令 / 场景 | 结果 |
| --- | --- |
| `go test -race ./internal/metadata ./internal/store ./internal/transport/http ./internal/integration -count=3` | 最终源码连续三轮通过；分别 7.324 / 22.822 / 18.229 / 8.568 秒（包测试总时长，不是接口延迟） |
| `npm run contracts:check`、`npm run baseline:check` | 通过；类型生成无漂移，97 个旧基线测试声明保留 |
| `go vet ./...`、`go build ./...`、`GOOS=linux GOARCH=amd64 go build ./...` | 通过；Linux 仅交叉编译，非运行验证 |
| `npm run format:check`、`gofmt -l`、`git diff --check` | 通过 / 无输出 |
| `go test ./internal/metadata -run '^TestSlowSnapshotConsumerDoesNotBlockSettingsOrLocalWrites$' -count=1 -v` | 通过；小型合成 DB 中 40 次保存 p95 约 0.252 ms，max 约 0.269 ms，期间旧 HTTP 消费者仍阻塞，新地址使用新 Token |
| `npm test` | 通过；TypeScript/Vite、全量 Go（旧 app 包 215.322 秒）、27 个旧 UI Playwright 场景（1.8 分钟） |
| `go test -race -count=1 ./...` | 全量通过，无 race；旧 app 包 433.282 秒。最终 URL 规范化补充亦经上方四包三轮 race 通过 |

上述慢源采样与其他全量测试并发，非空闲机器基准。最终 IPv6 URL 规范化补充另经上述四包三轮 race 及最终 build/vet 验证。

慢源测试直接消费 `ProviderSnapshot`，**不是尚未实现的三源 adapter/资料 worker**；其结果不代表整票慢源验收或 50,000 文件性能已达标。新 UI 未切换，旧浏览器回归不能作为新页面完成证据。

### 下次接续

1. 三源结构化 adapter 和受控 HTTP，真实字符串 ID/namespace、完整性与唯一性、中文回退、独立限流/错误脱敏。
2. 有界 worker、任务恢复/最多三次重试、候选/手动选择/刷新/状态 HTTP，以及成功/失败/补充结果完整的事务版本 CAS。现有配置快照不授权旧请求提交。
3. 有效快照/演职员/人物关联与重试，统一图片缓存/按 key 合并/下载预算/TTL/取消/类型大小及引用隔离。
4. 03 完整验收后才能 resolved 并推进 04；本轮没有认领 04–06。

未修改 schema、既有 ADR、CONTEXT 或生产 README/API 文档。未切换 CLI/前端/用户 DATA_DIR，未部署、重启用户服务、读取真实凭据或操作用户媒体。Docker CLI 不可用；容器、真实三源/NAS、外部播放器未验证。

## Progress — 完整后端收尾（2026-10-02）

继续完成而非再次拆小票。新增三源结构化 adapter、两个有界 worker、成功/失败/补充资料统一事务 CAS、有效快照/人物、统一图片与全部 03 新 HTTP 能力。完整交接见 [资料流水线](../../../docs/architecture/metadata-pipeline.md)。

- 22 个新增顶层测试（含参数化交错场景），覆盖真实 HTTP、SQLite FK 失败回滚、旧配置/目标/选择/策略/generation、人物名称与目录空间、重启、补充 TTL、图片合并/全局和来源上限/取消/改绑/移除/删除，以及新 HTTP 响应契约。
- 本轮发现并修正：目录策略重新排队残留旧重试字段、不可用目标错误排队、任务状态分页与 catalog revision 的耦合，以及补充内容快照竞争后任务可能滞留 running。当前 fresh schema 增加独立 `revisions.metadata`，无 migration/legacy 导入。
- 最后专项三轮 race 已通过：metadata 26.880s、providers 11.562s、store 33.507s、HTTP 28.332s、integration 8.539s（包总时长）。之后仅把慢源保存采样由单次扩大到 40 次；单测通过，p95 0.562ms、max 1.469ms。
- 首轮 `npm test` 全部通过，含 27 个旧 UI 场景；最终源码的第二轮完整 `npm test` / Go race 亦已通过，完整结果见 Answer。

40 次保存期间两个实际资料 worker 被受控 HTTP 阻塞；上述是小型合成库并发测试采样，不是 50,000 文件/真实服务 SLA。没有切换新 CLI/前端、部署、读取真实凭据或操作用户媒体。


## Answer

**03 已完成，Status: resolved。** 基线 `7d4ec8e`；本次完整交付资料后端，不再次停在增量。详细实现/消费接口见 [资料流水线交接](../../../docs/architecture/metadata-pipeline.md)。

### 验收结果

| 验收 | 实现与证据 |
| --- | --- |
| 三源优先级、中文回退、唯一性、冷却 | 新结构化 adapters；真实 HTTP contract 覆盖 TMDB/TVDB/MetaTube，截断/坏行/重复不能证明唯一；实际 worker 的 TMDB 不完整→TVDB 回退通过 |
| 原始身份与搜索只读 | 不建立整数 ID bridge；保留大数字字符串和 MetaTube 大小写/转义身份；电影限制、跨地址同 ID/人物隔离测试通过 |
| 配置即时生效与并发 | 复用首轮加密/保留/清除/revision/密钥验证；两个实际 worker 阻塞时 40 次保存不等待上游；旧成功和旧失败均不能覆写新配置 |
| 成功/失败/补充提交 CAS | 目标可用性/类型、subject/binding/policy/provider/catalog/generation 同事务校验；失效工作只处理自己的 generation；补充内容另校验 snapshot revision |
| 删除/改绑/策略/配置交错 | 可控交错覆盖全部版本维度；真实 scan/文件删除/手动改绑后的旧图片不能写回；FK 失败整事务回滚；新代次不会被旧 worker 清除 |
| 手动绑定、TTL、人物 | 到期保留 manual 而不返回过期内容；仅重试到期的可选部分、不延长原 TTL；name-only 不跨标题合并；人物只来自当前有效本地绑定，作品去重、优先可用、系列不重复单集 |
| 图片并发与安全 | 同 key 合并、全局 4/每源 2、命中不等慢图、独立等待者取消；真实失败负缓存；引用/快照/配置及当前可用绑定 CAS；1 MiB JSON / 5 MiB 图片、MIME+签名、重定向/任意 URL/凭据隔离测试通过 |
| 刷新/重启/HTTP | 2 worker，65s 目标/20s 来源、每 generation 最多三次尝试；恢复不退还 attempts；selection/refresh/status/person/works/asset GET+HEAD 均接入新契约且实测响应通过校验 |
| 单一新资料真源 | 结构化 snapshot/credits/assets；没有 payload 双真源、旧人物索引修补或 legacy 导入。既有旧 app 测试/入口暂留到 06 清理，非最终双栈交付 |

“页面配置即时生效”在本阶段以新设置 HTTP、实际新 worker/adapter 和旧 UI 回归验证；**新 UI 切换仍属 05，不以旧 Playwright 作为新页面完成证据**。

### 实际验证

环境：macOS arm64，Go 1.25.12、Node 26.7.0、npm 11.19.0、本机 Chrome。只用临时 DB、合成媒体与受控上游。

| 命令 | 最终结果 |
| --- | --- |
| `npm test` | 第二轮全量通过：契约生成检查、TypeScript/Vite、全量 Go、27 个 Playwright 场景（1.8m） |
| `go test -race -count=1 ./...` | 最终全量通过，无 race；旧 app 433.239s；metadata 11.145s、providers 4.838s、store 13.544s、HTTP 10.916s |
| `go test -race ./internal/metadata/... ./internal/store ./internal/transport/http ./internal/integration -count=3` | 三轮通过，包耗时见本轮 Progress；最终扩展 40 次保存采样也包含在上述最终全量 race 中 |
| `go test ./internal/metadata -run '^TestSlowWorkersBoundedSettingsImmediateAndOldResultsRejected$' -count=1 -v` | 通过；两个 worker 同时被上游阻塞，40 次设置保存 p95 0.562ms、max 1.469ms（小型合成库，不是生产 SLA） |
| `npm run contracts:check` / `npm run baseline:check` | 通过；无类型漂移，97 个旧基线声明保留 |
| `go vet ./...` / `go build ./...` / `GOOS=linux GOARCH=amd64 go build ./...` | 通过；Linux 为交叉编译，非运行验证 |
| `npm run format:check` / `gofmt -l` / `git diff --check` | 通过 / 无差异 |

首轮全量 race 曾揭示一个测试 fixture 直接改文件状态而未更新读投影，导致代表项断言随随机 ID 排序变化；已改为使用真实 `MarkFileDeleted` 用例，随后三轮专项和第二轮全量 race 均通过，没有跳过或删除测试。

### 交接与边界

- 04 现在可认领：消费有效 Match/人物关系、独立 revision 和有界人物作品页，继续做完整查询投影、SQL 次数/分页/50,000 文件性能验收。当前不宣称达到这些性能目标。
- 05/06 尚未认领：新前端/播放器切换和旧 app/兼容入口清理仍在其原票范围。
- 没有修改 CONTEXT/既有 ADR、切换 CLI/前端或用户 DATA_DIR，没有部署、重启用户服务、读取真实凭据或操作真实媒体。
- schema 仅增加 fresh-install 的 `revisions.metadata`；没有迁移旧数据。Docker CLI 不可用；容器运行、真实三源/NAS/外部播放器未验证。
