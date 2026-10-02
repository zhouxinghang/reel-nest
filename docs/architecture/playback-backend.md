# 重构阶段 05：播放与文件操作后端增量

日期：2026-10-02（Asia/Shanghai）。基线：`fc7b4c5`。对应 issue 05。

**这是 05 的后端增量，不是整票完成或生产切换。** 新 `app.NewRuntime` 已装配播放、进度、串流、字幕和删除 HTTP；`cmd/reelnest` 与用户使用的 React 页面仍为旧路径。05 保持 `claimed`，06 仍受阻塞。没有导入、迁移或清空现有数据，没有重启用户服务。

## Module 与调用边界

- `playback.Service`：`Open` / `Preflight` / `Progress` / `SaveProgress` / `StreamLink` / `ValidStreamToken` / `Subtitles` / `Subtitle` / `DeletionPreview` / `Delete`。无 HTTP、SQL、全库读取或上游网络。
- `mediafs.FS`：返回并复核真实文件句柄、遍历同目录字幕、核对删除目标及父目录、精确 `unlinkat(flags=0)`。不接受用户传入的任意删除路径。
- `catalog.WithMutation`：扫描与删除共用每库变更租约。同库忙立即返回 `LIBRARY_BUSY`，不同库与读取/进度不被阻塞；关闭会等待删除后的有界核对。
- `store`：沿用已有定向读取、有限数值进度事务、`MarkFileDeleted` 及扫描发布。未增加 schema、兼容迁移或第二份进度真源。
- `transport/http`：显式 DTO、会话/同源与串流权限分离、OpenAPI 请求验证和错误分类。Range 的标准 HTTP 表示不强行转换成 JSON。

`Files` 是已有受控文件能力的窄接口；生产只使用 `mediafs.FS`。测试包包装真实 unlink 来安排取消或注入不确定 I/O，**不以假的删除成功代替真实文件副作用**。`Mutations` 只暴露租约回调，不另建锁或业务仓储。

## HTTP 与前端消费约定

| 能力 | 新 interface | 消费注意 |
| --- | --- | --- |
| 预检 | `GET /api/media/{id}/playback` | 返回 `mediaId/contentPath/progress`；不存在进度为 `null`；核对本地文件，不保证浏览器支持编码，不写进度 |
| 内容 | `GET/HEAD /api/media/{id}/content` | `http.ServeContent` 从同一已核对句柄串流；原始 bytes、单段/多段 Range、If-Range；不整文件缓冲 |
| 进度 | `GET/PUT /api/media/{id}/progress` | GET 为 `{progress:null|Progress}`，PUT 返回保存后的 Progress；秒数有限且位置不超过时长；完成仅允许接近结尾，但前端仍须只在原生 ended 后发送 |
| 串流 | `POST /api/media/{id}/stream-link`、`GET/HEAD /stream/{id}?token=...` | 返回相对路径，浏览器按自己的 origin 解析；永不过期、指定文件只读；不信任 Host/代理头，Cookie 不替代串流 Token |
| 字幕 | `GET /api/media/{id}/subtitles?limit=&cursor=`、`GET .../subtitles/{subtitle}` | 默认 50、最大 100；列表有 `page`，不能假定所有字幕一次返回；内容是校验后的 UTF-8 WebVTT |
| 删除预览 | `POST /api/media/{id}/deletion-preview`，body `{}` | 展示 `mediaId/path/warning`；`expiresAt` 为 Unix 毫秒；确认有效期十分钟 |
| 删除提交 | `POST /api/media/{id}/delete`，body `{confirmation,confirm:true}` | **不是旧字段 `confirmed`**；无自动重试；HTTP 200 不等于“文件已删且索引已更新” |

所有时间遵循毫秒新契约。新增记录了媒体与串流 GET/HEAD 的标准条件响应 `304/412`，重新生成了 TypeScript 类型，没有引入旧字段别名。

错误分类：`FILE_UNAVAILABLE` → 410（已删除/缺失）；`FILE_CHANGED` → 409（版本/路径变化）；`FILE_ACCESS_DENIED` → 409（挂载或权限）；`SUBTITLE_INVALID` → 422；`DELETION_PREVIEW_EXPIRED` → 410；`DELETION_TARGET_CHANGED` / `LIBRARY_BUSY` → 409。不会把这些文件错误说成解码失败，OS 路径和原始错误不回显。

### 串流与进度

HMAC 使用安装级持久化 `stream.key`，与删除确认、字幕分页采用不同签名域。目标 ID 在同路径文件版本替换后变化；旧 URL 即使尚未重扫，也会被真实文件指纹阻止读取新文件。重启、注销或其他家庭浏览器登录不旋转串流 Token，也不自动写进度。

进度不取得媒体库变更锁。数据库事务验证目标及数值并覆盖最后有效接收的更新；删除后保留旧进度供查询，但拒绝新写入，成功重扫清理文件及依赖。前端的单在途/最新待提交队列、关闭最多三秒等仍待实施，不以这个后端事务冒充前端完成。

### 字幕的有界读取

- 保留原有同 stem 或单个语言后缀 `[A-Za-z0-9_-]+` 的 SRT/VTT 关联规则，不把 `title.zh.forced.vtt` 悄悄纳入新范围。
- 按 256 个目录项枚举；页面只保留 `limit+1` 候选（最大堆），不收集整目录数组。排序为文件名序，字幕 ID 绑定媒体 ID、名称和文件指纹。
- 游标绑定媒体 ID、limit、完整候选集合的顺序无关 SHA-256 摘要与最多五分钟有效期。文件变化/媒体错配/limit 变化/签名或期限错误 → `CURSOR_STALE`。这是有界内存遍历，不声称任意目录分页是 O(page-size) I/O 或文件系统原子快照。
- 检查 symlink/特殊文件、枚举与打开的 inode/大小/mtime 一致性，结束时复核父目录和视频；读取至多 2 MiB + 1 字节，再复核文件。字幕改变后旧 ID 不可用。
- 纯转换器校验 UTF-8/BOM/换行、时间顺序、VTT 位置设置；拒绝无效文本、空内容、STYLE/REGION 和超限源文件，不改写任何源字幕。

### 删除结果与生命周期

预览签名绑定媒体 ID、绝对路径、父目录身份、含 ctime 的文件版本及到期时间；它不是授权。两步接口均要求家庭会话，同源保护不因持有签名而省略。

1. 取得每库租约后重新读取目标，重新观察并验签。
2. 将预览到期时间并入 unlink 的 context deadline；`mediafs` 最后一次检查与 syscall 前均可因取消/到期停止。
3. **确认未执行 unlink** 的错误可使用普通 Error，但删除提交的错误标记 `retryable:false`。
4. `deleted` 后用脱离 HTTP/应用取消、最多十秒的 context 执行 `MarkFileDeleted`；租约保持到核对结束。若失败，返回 `fileOutcome:deleted,indexOutcome:pending,retryable:false`，不回滚或假称文件未删。
5. 不确定文件系统错误返回 `fileOutcome:unknown,indexOutcome:pending,retryable:false`。不从一次路径缺失猜测是本次操作删除，也不把替换文件当成原目标；提示人工核对/重新扫描。客户端连接丢失本身同样只能视为未知，不能直接重试。
6. 成功核对标记 deleted、更新投影并使受影响资料工作失效；下一次完整成功扫描才移除记录及依赖。字幕、图片、其他版本、相邻单集与目录不删除。

应用锁不能阻止宿主机/NAS 写入；POSIX 不提供原子的“仅当 inode 相同才 unlink”，最后复核与 unlink 间的外部竞争仍然存在。已打开的视频句柄可能继续读取。底层阻塞 NAS syscall 不能被 Go context 强行中断；十秒是索引核对预算，不是底层文件系统操作的硬实时保证。

## 验证追踪

新增 15 个 Go 顶层测试（playback 6、HTTP 8、mediafs 1），扩展已有 runtime 重启测试；新增 1 个真正使用新 runtime 的原生 `<video>` Playwright 场景。旧 97 个基线声明保留。

| 切面 | 证据 |
| --- | --- |
| GET/HEAD/Range/If-Range/条件响应、空进度/有限值/家庭共享/从头重播 | `internal/transport/http/playback_test.go`，实际响应逐项做 OpenAPI 校验 |
| 串流与家庭权限分离、同源写保护、文件替换/重扫后旧凭证失效 | 同上；真实不同客户端及临时文件 |
| 单视频真实 unlink、父目录/根/mtime/ctime/符号链接/FIFO/权限拒绝、取消不删 | HTTP 与 `internal/playback/playback_test.go`、`internal/mediafs/mediafs_test.go` |
| 文件已删但索引失败、事务回滚及扫描修复 | HTTP 测试在真实临时 SQLite 上添加失败触发器；检查实际文件和记录状态 |
| 请求取消后核对、到期恰好跨越最终检查、关闭等待、未知结果不重试 | playback 包包装真实文件能力安排执行交错；unknown 是受控故障注入，不是实机 NAS 故障复现 |
| 字幕转换/拒绝/无源写入、221 个字幕的多页边界及版本失效 | HTTP 测试；已移动父目录和取消的枚举拒绝另由 mediafs 测试覆盖 |
| 重启后会话、媒体 ID、进度、串流签名不变 | `internal/integration/runtime_test.go` 与 playback 重启测试 |
| 新后端的 VP8 实际解码、限速拖动后新 Range、原生字幕、注销后的无会话串流解码 | `tests/runtime/playback.spec.ts` + `tests/runtime-server`；消费生成类型、真实新 runtime/API/文件，无内部 mock |

原生 `<video>` fixture **不是新的产品播放器 UI**。它不覆盖 React Router/Query、设置草稿、前端进度队列、Blob 释放、自动播放失败交互或手机卡片布局。旧 27 个 UI 回归仍是防回归证据，不能据此勾选 05 的整项前端验收。

### 实际门禁

环境：macOS arm64、Go 1.25.12、Node 26.7.0、npm 11.19.0、本机 Chrome。所有媒体和数据库均在自动创建的临时目录，未使用真实凭据或媒体。

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

额外尝试直接对 Playwright 脚本运行独立 `tsc --types node`，仓库未安装 `@types/node`，返回 TS2688。这不是当前 `npm test` 的门禁，也没有被记为通过；未为这项额外检查安装依赖或伪造声明。现有生产前端的 `tsc -p web/tsconfig.json` 已通过；新原生脚本由 Playwright 实际执行。后续 05 若引入测试专用 TypeScript 门禁，应补测试环境类型并隔离浏览器/Node 全局类型。

## 后续 05 / 06 交接

1. 05：React Router / TanStack Query、统一请求取消/超时/401、URL 分组/分页、有界服务端状态订阅与精确失效；本轮没有安装这些依赖。
2. 05：播放器状态机、单在途+最新快照、最多三秒关闭等待及诚实保存提示；仅 native ended 才提交 completed。新字幕分页、Blob 释放、外部协议编码/复制兜底、两步删除的结果未知交互需接新 DTO。
3. 05：页面、卡片/CSS Modules、键盘/390px 全套新 UI 验证；完成后再切命令装配/静态资源，不引入旧契约兜底。
4. 06：可核查的旧代码清理候选是 `internal/app/{playback,stream,subtitles,deletion}.go` 及旧初始化；纯字幕转换器已迁入新 playback，但暂时保留旧消费者，待旧 UI/CLI 退出后整条删除。不可先删除旧安全回归；替代证据对应上表，新 UI 场景仍须补齐。
5. 最终性能门槛、当前态 README/API 重写、Docker 实测/切换回退等仍按原 06 处理，本轮不重复声称已完成。Docker CLI 不可用；Linux 交叉编译不等于容器运行。真实 NAS/EROFS、IINA/PotPlayer/VLC 实机、真实三源与跨设备兼容性未验证。
