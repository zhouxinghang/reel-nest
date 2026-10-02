# 阶段 03 增量：资料源设置与配置快照

> 历史增量记录：03 的后续完整后端实现见 [资料流水线交接](./metadata-pipeline.md)。下文“尚未实现”描述的是首个增量提交时的状态。

日期：2026-10-02（Asia/Shanghai）。对应 `.scratch/architecture-refactor/issues/03-metadata-providers-assets.md`。

这是 **03 的第一个可验证增量，不是 03 整体完成**。新 `app.NewRuntime` 提供设置 HTTP；`cmd/reelnest`、旧前端和正在运行的实例未切换。没有部署、迁移、真实资料源联通或用户媒体操作。

## 已实现的边界

- `metadata.Service` 负责凭据语义、AES-256-GCM、不可变配置注册表及保存时的短期串行化。无 legacy 环境变量导入，无网络请求、连通性探测或持网络锁的设置保存。
- `store.ProviderSettings` 一次读取三源配置。`store.SaveProviderSettings` 用一个真实 SQLite 事务检查 revision，保存密文/目录空间，递增受影响任务 generation 和 catalog revision；失败全部回滚。没有向 metadata 暴露 SQL/事务。
- `ProviderSnapshot` 是值拷贝，保存后发布新的整个注册表。旧消费者可继续持有旧值，新消费者立即取得新配置；调用者不能修改注册表。凭据不被默认 JSON/fmt 输出，HTTP 只使用显式 DTO。
- 加密 AAD 绑定 source、规范服务地址、配置 revision；空配置也加密，不能以有无密文判断 configured。独立密钥沿用阶段 01 的 0600 文件、启动检查和不自动重建规则。
- `GET /api/settings/metadata` 和 `PATCH /api/settings/metadata/{source}` 接入新版同源/家庭会话/16 KiB 请求校验及 OpenAPI 响应校验；`GET /api/libraries` 的资料源状态也来自同一注册表。串流形态凭据不能访问设置。

### 保存语义

- TMDB Token、TVDB Key/PIN、MetaTube Token：省略/null/空白保持，显式 clear 删除；非空值和 clear 不能同时提供。裁剪后最多 4096 UTF-8 字节，不接受内部空白/控制字符。
- 只有 TVDB 接受 pin/clearPin，只有 MetaTube 接受 url；HTTP 对不适用字段的 null/false 同样拒绝。
- revision 必须匹配；即使是无变化提交，旧 revision 也返回冲突。完全相同的保存不递增 revision，不重新排队任务。
- MetaTube 的 null URL 保持，空白 URL 清除。换地址时已有 Token 必须重新输入或显式清除，绝不默默携带旧 Token。
- configured 只表示配置齐备，不表示联网成功：TMDB/TVDB 要求主凭据，TVDB 只有 PIN 时不齐备，匿名 MetaTube 有地址就齐备。

### 目录空间

TMDB/TVDB 使用固定 API 地址和固定目录空间。MetaTube 地址规范化为显式 `/v1` API 后缀，保留反向代理前缀大小写；host 小写、IPv6 规范表示、默认端口/尾部斜杠去除。`http://nas/proxy` 与 `http://nas/proxy/v1/` 是同一空间。

支持管理员显式选择的内网 HTTP/HTTPS、ASCII 主机名/IP 和反向代理前缀。拒绝 userinfo、查询/片段、非法端口、目录穿越、编码分隔符/二次编码、空白/控制字符及歧义路径；保存过程不做 DNS 或 HTTP。

不同规范地址得到不同 `provider_catalogs` 行；Token 轮换不换空间。重新配置回已经存在的规范地址时，复用该地址的空间但**不恢复历史 Token**。该规则遵循 schema 的 `(source, base_url)` 唯一约束，不建立每次保存一个新空间的版本表。

### 设置事务中的任务更新

- 当前策略使用变更来源的自动目标：重新排队并递增 generation。
- 当前绑定属于活动目录空间的手动目标：重新排队刷新，但不改手动绑定。
- 手动目标绑定在该来源的旧空间：旧任务变为 superseded，保留原绑定和原快照 TTL，不能把旧 remote ID 用在新地址。
- 其他来源、无关媒体库及已删除的文件目标不因这次保存新增任务。
- 重新排队清空旧退避、错误和开始/结束时间，attempts 归零。领取时的版本元组及 provider 记录须由后续 worker 重新获取并完整覆盖，不能复用上一个 generation 的领取信息。

**这些设置事务不等于匹配提交已安全实现。** 在途结果仍必须在后续 `CommitMatch` 成功/失败/补充资料的同一事务核对 subject、binding、policy、provider revision/catalog 和 generation；不能只在网络请求前检查 `ProviderSnapshot`，或把内存比较当成事务 CAS。领取时也须核对数据库配置 revision 与所持快照一致；若恰好遇到 DB 提交到注册表发布的间隙，应重新取得快照/重试，不能混用新 revision 和旧凭据。

## 实际验证

仅临时 SQLite、合成数据库记录、受控 `httptest` HTTP 消费者及现有隔离浏览器 fixture。环境：macOS arm64、Go 1.25.12、Node 26.7.0、npm 11.19.0、本机 Chrome。

新增 14 个顶层测试，另扩展新 runtime 重启测试。覆盖：

- 配置持久化/重启、三源互不串值、默认序列化不泄密、数据库/WAL 不含明文；密文/来源/地址/revision 篡改和配置丢失均拒绝载入。
- 空输入、独立清除、字节上限、不合法字段、stale/no-op、24 路同时 CAS 保存以及关闭竞争。
- MetaTube 地址/Token 轮换/匿名配置/禁用/返回已知地址与不可变旧快照；URL 规范化幂等性及 IPv6。
- 新建目录空间、设置、任务、catalog revision 的两类事务失败注入及回滚；仅重排受影响目标；旧空间手动绑定和快照保留。
- 新 HTTP 契约、Cookie/同源保护、只读串流凭据隔离和错误不回显秘密；缺失密钥拒绝启动、不重建、恢复原密钥后可启动。
- 一个真实 HTTP 消费者持续阻塞时，40 次配置保存、目录/文件查询与进度写入继续完成，新地址仅取得新 Token。该消费者直接使用配置快照 seam，**不是尚未实现的 provider adapter/匹配 worker**，也不是 50,000 文件性能验收。

完整命令/最终结果见 issue 本轮进展记录；浏览器回归仍验证旧 UI，不代表新设置页完成。

## 后续仍须完成

1. 结构化三源 adapter：Search/FetchTitle 的真实字符串标识、完整性/唯一性、中文回退、受控 API/图片通道、来源限流与错误脱敏；不得恢复 MetaTube 整数映射。
2. 2-worker 有界任务、重启恢复/最多三次重试、手动选择/显式刷新/状态 HTTP，以及全部结果的版本条件提交和可控交错测试。
3. 结构化快照/演职员、本地有效人物关联和补充内容重试；过期不能抹掉手动选择或展示旧内容。
4. 统一按 key 合并的图片下载缓存、4 全局/2 同源预算、TTL/取消/配置隔离及 MIME/大小/重定向防护。
5. 阶段 04 的有效资料查询/人物分页和性能，阶段 05 的新设置页面/前端切换，阶段 06 的旧实现清理与交付。旧代码还在是分阶段实施状态，不是最终双栈交付。

没有启动资料 worker，因此当前扫描/设置入队的资料任务只持久化等待后续实现；不宣称已有自动匹配、旧结果拒绝提交或图片隔离的端到端证据。Docker CLI 不可用；真实三源、容器、NAS、外部播放器未验证。
