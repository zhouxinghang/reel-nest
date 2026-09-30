# 核心家庭媒体管理系统：Ticket 索引

发布日期：2026-09-30。用户已确认以下 9 个纵向切片及依赖。后续按用户要求开始实施，当前已完成 01–09；Docker 校验按用户要求跳过。

## 执行依据

- 父规格：[核心家庭媒体管理系统](/Users/bytedance/workspace/reel-nest/.scratch/core-media-library/spec.md)，保持原内容及 ready-for-agent 状态，不因拆票而关闭或修改。
- 领域术语：[家庭媒体管理](/Users/bytedance/workspace/reel-nest/CONTEXT.md)。
- [ADR 0001：源文件删除限定为单文件永久删除](/Users/bytedance/workspace/reel-nest/docs/adr/0001-single-file-permanent-deletion.md)。
- [ADR 0002：以宿主机挂载目录作为媒体接入边界](/Users/bytedance/workspace/reel-nest/docs/adr/0002-host-mounted-media-directories.md)。
- [ADR 0003：外部播放器使用不过期的单文件只读链接](/Users/bytedance/workspace/reel-nest/docs/adr/0003-non-expiring-read-only-stream-links.md)。

正式规格与已接受 ADR 高于历史讨论。Go、React/TypeScript、SQLite、Docker Compose 及精细工程默认值只是推荐，不是新增的已确认技术约束；单机 Docker、功能范围与授权/删除边界是约束。发布时仓库尚无应用代码；现已采用 Go、React/TypeScript、SQLite 实现 01–09，不新增技术锁定 ADR。

## Tickets 与依赖

| 编号 | Ticket | Blocked by | Status |
| --- | --- | --- | --- |
| 01 | [单机部署与家庭共享账号登录](/Users/bytedance/workspace/reel-nest/.scratch/core-media-library/issues/01-household-login-and-deployment.md) | 无 | resolved |
| 02 | [媒体目录扫描与基础媒体库浏览](/Users/bytedance/workspace/reel-nest/.scratch/core-media-library/issues/02-media-directory-scan-and-library.md) | 01 | resolved |
| 03 | [电视剧、季与单集组织](/Users/bytedance/workspace/reel-nest/.scratch/core-media-library/issues/03-series-seasons-and-episodes.md) | 02 | resolved |
| 04 | [浏览器直放与家庭共用续播](/Users/bytedance/workspace/reel-nest/.scratch/core-media-library/issues/04-browser-playback-and-progress.md) | 02 | resolved |
| 05 | [IINA/PotPlayer 单文件只读串流](/Users/bytedance/workspace/reel-nest/.scratch/core-media-library/issues/05-external-player-streaming.md) | 04 | resolved |
| 06 | [网页外挂字幕选择与关闭](/Users/bytedance/workspace/reel-nest/.scratch/core-media-library/issues/06-browser-sidecar-subtitles.md) | 04 | resolved |
| 07 | [TMDB 影片资料与故障降级](/Users/bytedance/workspace/reel-nest/.scratch/core-media-library/issues/07-tmdb-metadata-and-fallback.md) | 03, 04 | resolved |
| 08 | [手动修正影片资料匹配](/Users/bytedance/workspace/reel-nest/.scratch/core-media-library/issues/08-manual-metadata-matching.md) | 07 | resolved |
| 09 | [安全地永久删除单个源文件](/Users/bytedance/workspace/reel-nest/.scratch/core-media-library/issues/09-single-file-permanent-deletion.md) | 02 | resolved |

所有依赖均指向更早编号，且只列真正阻塞本项的能力。每项交付自身必要的持久化、接口、界面、测试及使用说明，不拆出横向“后端/前端/测试”任务。

## Frontier

- 已完成：**01–09**（resolved）；Docker 实测按用户要求跳过并保留未验证记录，IINA/PotPlayer 实机结果单独列为未验证。
- 当前 frontier：**无**。9 个 implementation tickets 均已 resolved，无 claimed ticket；父规格保持原 ready-for-agent 状态，Docker/实机未验证项不伪报通过。
- 最新复核（2026-10-01，基线 `75ab30c`）：9 个 ticket 与索引状态一致，非 Docker 全量回归通过，未发现需要重开的事项；各 ticket 已追加复核记录。01 的 Docker 实测验收继续保留未勾选。
- 领取时将对应 ticket 的 Status 改为 claimed；完成时追加 Answer、标记 resolved 并更新本索引。未完成的阻塞项不能跳过。

依赖说明：07 必须用实际浏览器播放验证 TMDB 故障降级，不能仅展示播放入口，因此依赖 04；09 只依赖稳定媒体文件身份与基础详情，不需要等待 TMDB、电视剧组织或播放器。05 复用 04 的媒体读取契约，06 复用 04 的网页播放器。

## 跨切片验收归属

- 重复扫描的稳定身份由 02 建立；04 补充观看进度在重扫/重启后的保留；07 补充匹配关联保留；08 补充手动选择不被重扫覆盖。
- 故事 30：04 负责浏览器失败反馈与外部播放器建议，05 负责可用的唤起/复制兜底及失败入口衔接。
- 05 建立单文件只读串流授权，09 的删除始终要求家庭共享账号。两项都存在时，后完成的一项补充“有效串流凭证不能删除”的应用边界回归；不得据此给 05 和 09 制造互相阻塞。
- 09 与播放/串流能力都存在时，后完成的一项检查删除后读取返回文件不可用。某一项尚未实现时，通过真实文件缺失场景验收已有读取契约，不要求提前实现另一项。
- 测试与使用说明随能力增量交付；整体交付验收还需串起登录、目录接入、扫描、媒体浏览、详情/单集、播放/续播/字幕、外部播放器以及安全删除。整体主流程通过不等于任一实机环境已验证。

## User Stories 覆盖

以下编号对应父规格的 60 个 User Stories。共同故事由多项增量完成，表中出现不表示单项独立覆盖该故事的全部后续能力。

| Ticket | 父规格 User Stories |
| --- | --- |
| 01 | 1, 2, 3, 5, 55, 57, 58, 60 |
| 02 | 3, 6, 7, 8, 9, 10, 11, 12, 13, 18, 19, 20, 24, 56, 57, 58 |
| 03 | 14, 16, 17, 18, 19, 20 |
| 04 | 1, 4, 11, 28, 29, 30, 31, 32, 57 |
| 05 | 30, 36, 37, 38, 39, 40, 41, 42, 43, 44, 45, 58, 59 |
| 06 | 33, 34, 35 |
| 07 | 15, 21, 22, 23, 24, 25, 26, 57, 58 |
| 08 | 27 |
| 09 | 46, 47, 48, 49, 50, 51, 52, 53, 54, 56 |

覆盖编号合并后为 1–60，无遗漏。尤其保持：仅浏览器写家庭共用观看进度、未匹配媒体仍可观看、串流链接不过期且不授予管理权、删除仅作用于一个真实视频文件。

## 统一验证规则与现有限制

- 以运行中应用的外部边界为主要测试切面：公开 HTTP、少量浏览器测试、真实临时媒体目录与持久化存储。只替换外部 TMDB 服务，不模拟内部扫描、授权、文件删除或数据库函数来证明成功。
- 异步扫描通过可观察状态等待。需要真实文件副作用的断言检查文件结果；需要真实播放的断言不能只检查链接、按钮或成功状态码。
- 删除测试和演示始终使用专门创建的隔离临时媒体；本次确认不授权对用户现有媒体执行删除。
- 自动化测试、真实 TMDB 联通、Docker 运行、浏览器播放及 IINA/PotPlayer 实机兼容性分别记录环境、结果与未验证项。此前环境探查未在 PATH 找到 Docker CLI，也没有 Windows/PotPlayer 实机通过记录；实施时重新检查，不虚构通过结果。
- 没有已约定的容量、并发或延迟指标，不自行声称达标；浏览器格式兼容、中文资料覆盖和 PotPlayer 协议注册均不做普遍保证。

## 发布审计（历史）

父规格发布前的 SHA-256：b59d28b65b485cead460fbf863830c6b99ee371a9666d57d8ca33cadc85747ff。

发布时仅新增本索引与 9 个独立 ticket；没有修改父规格、实现代码或执行媒体操作，当时所有验收复选框保持未完成。

## 实施记录

- 2026-09-30：01、02 已交付并回填 Answer/验收状态；父规格及 ADR 保持不变。
- 通过构建、真实应用 HTTP、race/vet、Chrome 浏览器及 Linux/amd64 交叉编译验证；完整记录见 [验证记录](/Users/bytedance/workspace/reel-nest/docs/verification.md)。
- Docker 镜像构建和容器运行按用户要求跳过，不是通过；01 的容器验收保留未勾选，不阻塞此次明确要求的继续推进。
- 01、02 阶段测试仅使用隔离临时媒体，未触碰用户媒体；当时 03–09 后续能力及真实播放/TMDB/外部播放器兼容性未验证。
- 2026-09-30：03 已交付并回填 Answer/全部验收状态；新增持久化本地电视剧 → 季 → 单集组织、保守文件名识别、数值排序、独立文件版本及未组织回退，未匹配媒体仍完整可浏览。
- 03 增量通过 12 个应用级用例（新增 4 个，含 25 种命名子用例）、race/vet、Chrome 季集导航及桌面/窄屏布局检查、构建/格式/依赖审计和 Linux/amd64 交叉编译；真实 02 旧版 → 新版的离线媒体升级与再次重启验证通过，保留账号、媒体文件标识和组织结果。
- 03 未修改父规格或 ADR，未操作用户媒体；Docker 继续明确跳过，不是通过。04–09 未实现，下一顺序 frontier 为 04，09 亦已解除阻塞。

- 2026-09-30：本轮完成 04–06，回填 Answer/验收状态。原始视频范围读取、家庭共用进度、不过期单文件只读串流和 UTF-8 SRT/VTT 字幕已交付；通过 17 个 Go 应用级用例、4 个 Chrome 流程及新增 3 流程×3 次重复。真实 03→新版离线升级/恢复/进度/独立串流/再次重启通过，父规格和 ADR 未修改。
- 04–06 的可播放样本为项目自生成 60 秒无音轨 VP8/WebM；浏览器实际解码、限速下拖动后新 Range 请求及原生字幕显示/关闭均验证，未触碰用户媒体。
- Docker 继续明确跳过；IINA/macOS 与 Windows/PotPlayer 实机、跨设备局域网、真实 NAS、其他编码/音轨及真实 TMDB 未验证。协议和 HTTP 通过不等于实机通过。下一顺序 frontier 为 07，09 也可领取；09 落地需补真实删除接口的串流凭证隔离及删除后读取失败回归。


- 2026-09-30：本轮完成 07–08 并回填 Answer/验收状态：TMDB 中文优先、英文/本地回退、30 天缓存、可观察失败与手动修正已交付；扫描/播放与资料故障隔离，自动匹配不覆盖用户选择。
- 通过 25 个 Go 应用级用例（另有进程辅助入口）、全量 race/vet、7 个 Chrome 流程和资料流程 3×3 次重复；构建/格式/依赖审计/Linux 交叉编译及真实旧播放版本→资料版本离线升级通过。人工检查桌面海报墙与窄屏季集/匹配对话框。
- 无 TMDB 凭据、可选凭据文件缺失及服务失败时均通过实际浏览器视频解码验证；测试只替换外部 TMDB，不冒充真实联通。不操作用户媒体，父规格 SHA-256 与发布时一致，ADR 未修改。
- Docker 继续明确跳过；真实 TMDB API、NAS/跨设备和 IINA/PotPlayer 实机仍未验证。09 安全永久删除保留 ready-for-agent，是剩余唯一未完成切片。


- 2026-10-01：完成最后的 09，回填全部验收状态和 Answer；两步路径确认、单视频永久删除、目标复核、账号/只读凭证隔离及真实索引失败反馈已交付。默认媒体挂载仍只读，可按说明显式启用写入；无批量或连带删除。
- 31 个 Go 应用级测试、全量 race/vet、10 个 Chrome 流程、3 个新增删除流程×3 次重复、构建/格式/依赖审计/Linux 交叉编译均通过。真实旧资料版本→删除版本的离线升级、状态保留、单文件实际删除及重启通过。浏览器测试已隔离到各自应用/数据库，旧空库验收完整保留。
- 所有删除/故障测试仅使用隔离临时文件和真实 SQLite；覆盖文件已删但数据库写锁阻止更新以及扫描修复。没有操作用户现有媒体。Docker 继续按要求跳过；真实 NAS、只读挂载 EROFS、TMDB 和 IINA/PotPlayer 实机仍未验证。最终核对与 POSIX unlink 的外部写入竞态边界见 README。
- 01–09 全部 resolved，当前无 frontier。父规格 SHA-256 与发布时一致，领域术语及 ADR 不变；本轮完整记录见 [验证记录](/Users/bytedance/workspace/reel-nest/docs/verification.md)。

- 2026-10-01（收尾复核）：本轮开始时 01–09 已全部 resolved，不重复领取或扩大功能范围。重新通过禁用缓存的 31 个 Go 应用用例、全量 race/vet、10 个 Chrome 流程、生产构建/格式/依赖审计、Linux 交叉编译及真实旧资料版本→当前版本升级/删除/重启验证，未发现回归问题。
- 本轮仅更新验证与各 issue 的复核记录，状态仍全部为 resolved；Docker 按用户要求跳过，其他实机未验证项保持原样。父规格及领域/ADR 文档未修改，当前仍无 frontier；详情见验证记录的「01–09 收尾复核」节。
