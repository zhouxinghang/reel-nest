# 核心家庭媒体管理系统：Ticket 索引

发布日期：2026-09-30。用户已确认原始 9 个纵向切片及依赖；2026-10-01 追加扫描同步、数字单集识别及 Emby 首页/收藏/最近添加，当前已完成 01–18（含按目录配置 TMDB / TVDB 资料源、演员/导演信息、人物照片/关联作品、人物页性能优化、多类型影片图片、MetaTube 及具名媒体库）。Docker 校验按用户要求跳过。

## 执行依据

- 父规格：[核心家庭媒体管理系统](/Users/bytedance/workspace/reel-nest/.scratch/core-media-library/spec.md)，保持原内容及 ready-for-agent 状态，不因拆票而关闭或修改。
- 领域术语：[家庭媒体管理](/Users/bytedance/workspace/reel-nest/CONTEXT.md)。
- [ADR 0001：源文件删除限定为单文件永久删除](/Users/bytedance/workspace/reel-nest/docs/adr/0001-single-file-permanent-deletion.md)。
- [ADR 0002：以宿主机挂载目录作为媒体接入边界](/Users/bytedance/workspace/reel-nest/docs/adr/0002-host-mounted-media-directories.md)。
- [ADR 0003：外部播放器使用不过期的单文件只读链接](/Users/bytedance/workspace/reel-nest/docs/adr/0003-non-expiring-read-only-stream-links.md)。

正式规格与已接受 ADR 高于历史讨论。Go、React/TypeScript、SQLite、Docker Compose 及精细工程默认值只是推荐，不是新增的已确认技术约束；单机 Docker、功能范围与授权/删除边界是约束。发布时仓库尚无应用代码；现已采用 Go、React/TypeScript、SQLite 实现 01–18，不新增技术锁定 ADR。

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
| 10 | [扫描同步移除与数字单集识别](/Users/bytedance/workspace/reel-nest/.scratch/core-media-library/issues/10-scan-sync-and-numeric-episodes.md) | 02, 03 | resolved |
| 11 | [Emby 首页、家庭收藏与最近添加](/Users/bytedance/workspace/reel-nest/.scratch/core-media-library/issues/11-emby-home-favorites-recent.md) | 02, 03, 04 | resolved |
| 12 | [目录影片资料源与优先级](/Users/bytedance/workspace/reel-nest/.scratch/core-media-library/issues/12-directory-metadata-sources.md) | 07, 08 | resolved |
| 13 | [演员、导演信息](/Users/bytedance/workspace/reel-nest/.scratch/core-media-library/issues/13-cast-and-directors.md) | 12 | resolved |
| 14 | [人物照片与关联作品](/Users/bytedance/workspace/reel-nest/.scratch/core-media-library/issues/14-person-photos-and-works.md) | 13 | resolved |
| 15 | [人物关联作品页性能优化](/Users/bytedance/workspace/reel-nest/.scratch/core-media-library/issues/15-person-page-performance.md) | 14 | resolved |
| 16 | [多类型影片图片](/Users/bytedance/workspace/reel-nest/.scratch/core-media-library/issues/16-title-artwork.md) | 12, 14 | resolved |
| 17 | [MetaTube 影片资料源](/Users/bytedance/workspace/reel-nest/.scratch/core-media-library/issues/17-metatube-provider.md) | 12, 16 | resolved |
| 18 | [按目录分组的具名媒体库](/Users/bytedance/workspace/reel-nest/.scratch/core-media-library/issues/18-named-directory-libraries.md) | 02, 11, 12 | resolved |

所有依赖均指向更早编号，且只列真正阻塞本项的能力。每项交付自身必要的持久化、接口、界面、测试及使用说明，不拆出横向“后端/前端/测试”任务。

## Frontier

- 已完成：**01–18**（resolved）；Docker 实测按用户要求跳过并保留未验证记录，IINA/PotPlayer 实机结果单独列为未验证。
- 当前 frontier：**无**。01–18 均已 resolved，无 claimed ticket；父规格与既有未验证边界不变。
- 最新变更（2026-10-02）：18 个 ticket 与索引状态一致；18 增加目录对应的具名媒体库、名称迁移/修改、单库浏览与按目录分组展示；后续按用户要求将首页及完整“最近添加”改为跨目录统一排序，不影响具名媒体库入口。01 的 Docker 实测验收继续保留未勾选。
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
| 10 | 7, 14, 16, 18, 19 |

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

- 2026-10-01：完成 10。完整成功扫描改为事务性移除源目录中已不存在的媒体记录，同时清理观看进度、文件级资料关联及空电视剧；扫描失败/中断仍保留旧索引。电视剧目录新增“作品目录/纯数字文件名”规则，并支持从直接季目录取得季号。
- 新增 2 个应用级测试，总计 33 个；全量 `npm test`（含 10 个 Chrome 流程）、race、vet、格式与 diff 检查通过。另以临时应用数据对 `/Users/bytedance/Downloads/test2` 进行只读公开 HTTP 扫描，确认 `太平年/01.mp4` 识别为第 1 季第 1 集，未修改用户媒体。
- 01–10 全部 resolved，当前无 frontier；Docker 与其他实机未验证项保持不变。

- 2026-10-01：完成 11，新增 Emby 风格全宽首页、家庭收藏和真实最近添加；旧库收录日期保持未知，收藏与观看进度独立，串流凭证不能修改收藏。
- 全套 14 个 Chrome 流程、Go 应用测试、race/vet、格式与 diff 检查通过；本地 8090 已备份数据库并更新，实际桌面/窄屏界面通过检查，未改变用户媒体和观看进度。参考存档缺少首页画面，不声称逐像素一致；01–11 全部 resolved，当前无 frontier。


- 2026-10-01：完成 12；按媒体目录独立配置 TMDB / TVDB 启用与顺序，顺序下载资料/海报、失败回退、手动匹配来源标识及旧 TMDB 数据原子迁移。自动配置变化不覆盖手动选择，不改写媒体文件或进度。
- 最终 `npm test`（42 个应用测试、1 个启动器测试、16 个 Chrome 流程）、全量及最终资料增量 race、vet、格式与 diff 检查通过；目视检查目录资料源窄屏截图。真实 TMDB/TVDB 与 Docker 仍未实测，用户服务未重启；既有工作区改动完整保留，01–12 全部 resolved。


- 2026-10-01：完成 13；电影/电视剧演职员资料与来源内去重、角色合并、导演过滤、旧缓存补齐及 24 小时失败重试间隔已交付。单集明确为整部电视剧资料，自动/手动匹配均接入，不改变媒体、进度、手动选择和已有资料期限。
- 45 个 Go 应用测试、启动器测试、17 个 Chrome 流程、全量及最终演职员增量 race、vet、构建、格式/diff 检查通过，目视检查桌面及 390px 长名单。README/API/验证记录已更新；真实两源联通及 Docker 未实测，用户服务未重启，01–13 全部 resolved，当前无 frontier。
- 2026-10-01 15:24：按用户后续“重启服务”要求，完整备份后构建并重启本地 8090，健康检查/新版前端核验通过，原数据保留；现有 2 条 TMDB 资料的演职员已实际补齐，TVDB 尚未配置凭据。详见验证记录部署补充节；ticket 状态不变。


- 2026-10-01：完成 14，增加演员/导演照片、人物页及本地关联作品跳转；同名/跨源不误合并，电视剧去重，旧缓存无损补齐，照片独立缓存与故障退避。
- 50 个 Go 应用测试、启动器测试、20 个 Chrome 流程、全量及最终人物/演职员增量 race、vet、构建与格式/diff 检查通过。桌面/390px/长姓名已目视检查；真实两源照片及 Docker/其他实机未验证，未操作用户媒体或重启现有服务。01–14 全部 resolved，当前无 frontier。
- 2026-10-02：完成 15；人物关联作品由全库 JSON 扫描改为按来源和人物 key 的持久派生索引，照片关联改为轻量查询。5000 部隔离作品的目标人物 HTTP 查询从约 2.69 秒降至约 6 毫秒；全量 Go、人物 race、9 个资料浏览器流程、vet 与生产构建通过。未操作用户媒体或重启现有服务，01–15 全部 resolved。

- 2026-10-02：完成 16；Emby 分类的竖版海报、横版封面及详情背景、两源按需获取/缓存、缺图回退及旧有效资料补齐已交付。全量 Go / race、23 个浏览器流程、vet、构建和格式/diff 检查通过，最终图片专项回归通过，桌面/窄屏已目视检查。未操作用户媒体或重启现有服务，01–16 全部 resolved。

- 2026-10-02 00:32：按用户要求备份并重启本地 8090，新 PID 6113 健康且使用新版静态资源；原数据保留，2 条 TMDB 资料已补齐封面/背景引用。未主动扫描、播放、下载横图或改写用户媒体，ticket 状态不变；部署边界见验证记录。

- 2026-10-02：完成 17；MetaTube 电影搜索/详情/编号匹配/演职员姓名/三类图片、服务地址与 Token 配置、目录显式启用和回退已交付。全量 Go/race/vet、24 个 Chrome 流程、专项 race 与浏览器两次重复、构建和格式/diff 检查通过。没有调用真实服务、重启用户应用或改写用户媒体；真实 MetaTube 与 Docker 部署未验证，01–17 全部 resolved。

- 2026-10-02 09:42：按用户要求备份并重启应用，8090 新 PID 48308 健康，新版 MetaTube 页面和存储已生效；原有业务数据保持一致，MetaTube 尚未配置。17 状态仍 resolved，部署记录见 docs/verification.md。


- 2026-10-02：完成 18；媒体目录与具名媒体库一一对应，支持名称必填/修改、旧库离线名称迁移、首页目录卡片及单库浏览。全局搜索/分类/收藏/最近添加按目录分组，电影/电视剧详情显示所属媒体库。
- 全量 Go（64 个应用测试及启动器测试）、25 个 Chrome 流程、最终相关 race、vet、生产构建、格式与 diff 检查通过；新增流程独立运行两次，桌面/窄屏已目视检查。未操作用户媒体、调用真实外部服务或重启现有应用，01–18 全部 resolved。

- 2026-10-02（18 后续调整）：按用户要求将首页及完整“最近添加”改为跨目录统一展示，保留具名媒体库入口和其他浏览页分组。专项 5 个及全量 26 个浏览器流程、生产构建、Go vet、格式与 diff 检查通过；桌面/窄屏截图已检查。仅调整前端，无数据迁移；用户授权提交后备份并重启应用。
