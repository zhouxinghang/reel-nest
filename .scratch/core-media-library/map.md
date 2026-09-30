# 核心家庭媒体管理系统：Ticket 索引

发布日期：2026-09-30。用户已确认以下 9 个纵向切片及依赖；本次仅发布任务，不开始实现。

## 执行依据

- 父规格：[核心家庭媒体管理系统](/Users/bytedance/workspace/reel-nest/.scratch/core-media-library/spec.md)，保持原内容及 ready-for-agent 状态，不因拆票而关闭或修改。
- 领域术语：[家庭媒体管理](/Users/bytedance/workspace/reel-nest/CONTEXT.md)。
- [ADR 0001：源文件删除限定为单文件永久删除](/Users/bytedance/workspace/reel-nest/docs/adr/0001-single-file-permanent-deletion.md)。
- [ADR 0002：以宿主机挂载目录作为媒体接入边界](/Users/bytedance/workspace/reel-nest/docs/adr/0002-host-mounted-media-directories.md)。
- [ADR 0003：外部播放器使用不过期的单文件只读链接](/Users/bytedance/workspace/reel-nest/docs/adr/0003-non-expiring-read-only-stream-links.md)。

正式规格与已接受 ADR 高于历史讨论。Go、React/TypeScript、SQLite、Docker Compose 及精细工程默认值只是推荐，不是新增的已确认技术约束；单机 Docker、功能范围与授权/删除边界是约束。仓库暂无应用代码，不需要前置重构任务。

## Tickets 与依赖

| 编号 | Ticket | Blocked by | Status |
| --- | --- | --- | --- |
| 01 | [单机部署与家庭共享账号登录](/Users/bytedance/workspace/reel-nest/.scratch/core-media-library/issues/01-household-login-and-deployment.md) | 无 | ready-for-agent |
| 02 | [媒体目录扫描与基础媒体库浏览](/Users/bytedance/workspace/reel-nest/.scratch/core-media-library/issues/02-media-directory-scan-and-library.md) | 01 | ready-for-agent |
| 03 | [电视剧、季与单集组织](/Users/bytedance/workspace/reel-nest/.scratch/core-media-library/issues/03-series-seasons-and-episodes.md) | 02 | ready-for-agent |
| 04 | [浏览器直放与家庭共用续播](/Users/bytedance/workspace/reel-nest/.scratch/core-media-library/issues/04-browser-playback-and-progress.md) | 02 | ready-for-agent |
| 05 | [IINA/PotPlayer 单文件只读串流](/Users/bytedance/workspace/reel-nest/.scratch/core-media-library/issues/05-external-player-streaming.md) | 04 | ready-for-agent |
| 06 | [网页外挂字幕选择与关闭](/Users/bytedance/workspace/reel-nest/.scratch/core-media-library/issues/06-browser-sidecar-subtitles.md) | 04 | ready-for-agent |
| 07 | [TMDB 影片资料与故障降级](/Users/bytedance/workspace/reel-nest/.scratch/core-media-library/issues/07-tmdb-metadata-and-fallback.md) | 03, 04 | ready-for-agent |
| 08 | [手动修正影片资料匹配](/Users/bytedance/workspace/reel-nest/.scratch/core-media-library/issues/08-manual-metadata-matching.md) | 07 | ready-for-agent |
| 09 | [安全地永久删除单个源文件](/Users/bytedance/workspace/reel-nest/.scratch/core-media-library/issues/09-single-file-permanent-deletion.md) | 02 | ready-for-agent |

所有依赖均指向更早编号，且只列真正阻塞本项的能力。每项交付自身必要的持久化、接口、界面、测试及使用说明，不拆出横向“后端/前端/测试”任务。

## Frontier

- 当前可领取：**01**。所有任务均未领取、未实现，ready-for-agent 不代表依赖已解除。
- 01 完成后：02。
- 02 完成后：03、04、09 可独立推进；按编号领取时下一项为 03。
- 04 完成后：05、06 可推进，不等待 TMDB。
- 03 与 04 均完成后：07。07 完成后：08。
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

## 发布审计

父规格发布前的 SHA-256：b59d28b65b485cead460fbf863830c6b99ee371a9666d57d8ca33cadc85747ff。

本次只新增本索引与 9 个独立 ticket；没有修改父规格、实现代码或执行媒体操作。所有验收复选框保持未完成。
