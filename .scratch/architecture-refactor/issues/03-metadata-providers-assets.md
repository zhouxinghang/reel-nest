# 03：资料源、版本提交、人物与图片

Status: claimed
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

- [ ] 三源按目录优先级匹配；中文回退、候选完整性与唯一性、来源独立限流/冷却符合既有需求。
- [ ] MetaTube 仅电影，provider/remote_id 保持真实身份，搜索不写库；同 ID 跨来源/服务地址不会混用。
- [ ] 页面配置成功后即时生效；空输入保持/显式清除/旧 revision 冲突/换地址 Token 保护/密钥缺失拒绝启动均通过。
- [ ] 慢上游不使设置保存等待网络；成功和失败提交均核对 subject、binding、policy、provider 和 job generation。
- [ ] 目标被扫描移除、手动改绑、策略/配置变化后，旧任务不能复活目标、清除新任务或覆盖新选择；以可控交错测试验证。
- [ ] 缓存到期不清除手动选择，不返回过期内容；人物只来自当前有效本地关联，不按同名猜身份。
- [ ] 图片按 key 合并、全局/同源有界；命中不等慢图；取消不写负缓存；旧引用/旧配置结果不能污染新图。
- [ ] 上游响应体、图片大小/类型、重定向、任意 URL 和凭据隔离保持安全；无凭据及上游失败不影响本地读取。
- [ ] 不保留 payload 双真源、旧人物索引补丁或 legacy 配置迁入路径；有效刷新和失败重试能力保留。

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
