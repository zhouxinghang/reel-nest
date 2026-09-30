# 核心家庭媒体管理系统

Status: ready-for-agent

## Problem Statement

用户在本机或 NAS 上已有电影和电视剧，希望个人及家人能在局域网内方便地整理、浏览、播放并继续观看这些内容，而不是每次手动寻找文件。用户需要类似 Emby 的核心体验，但不希望承担完整媒体平台、服务器转码、多用户权限或网络存储管理的复杂度。

浏览器无法兼容所有片源，因此系统还需支持将媒体串流交给观看者设备上的 IINA、PotPlayer。用户同时需要释放不再保留的视频所占空间：“删除”必须操作实际源文件，而不是仅隐藏媒体库记录；这一能力必须明确展示风险和操作对象。

## Solution

提供单机 Docker 部署的家庭媒体管理系统，通过宿主机挂载的媒体目录接入本机或 NAS 文件。家庭成员使用一个家庭共享账号，在网页上完成以下闭环：

- 添加媒体目录，自动扫描一次；之后通过手动重新扫描发现变更。
- 在海报墙浏览电影和电视剧，电视剧按“电视剧 → 季 → 单集”组织。
- 从 TMDB 获取中文优先的影片资料和海报；缺少凭据、网络失败或无法匹配时，仍以文件名和占位图收录，并允许播放与手动重新匹配。
- 浏览器直接播放其支持的媒体，保存家庭共用观看进度，并支持同名外挂 UTF-8 SRT/VTT 字幕。
- 在兼容环境一键打开 IINA、PotPlayer，始终保留复制串流地址的兜底。串流地址不可猜测、仅能读取指定媒体文件且不自动过期；不回传外部播放器的观看进度。
- 展示目标文件路径，经二次确认后永久删除一个视频文件；不批量删除、不连带删除字幕或海报，也不提供回收站。

本规格覆盖已确认的核心范围，并将必要的异常处理、测试切面和实现建议显式列出。此前仅建议的技术栈、页面划分及精细默认值不伪装成用户已确认的硬约束。

## User Stories

以下角色仅代表使用场景：household viewer、library maintainer 和 deployer 不构成多个账号或不同权限等级。英文领域名称与项目术语表一致。

### 家庭身份与访问

1. As a household viewer, I want to access the Media Library from a browser on the local network, so that I can watch existing household media without installing a dedicated web client.
2. As a household viewer, I want to sign in with the Household Account, so that browsing and management are not available to an unauthenticated visitor.
3. As a household viewer, I want household members to share one Media Library, so that we do not have to maintain separate collections.
4. As a household viewer, I want to share Watch Progress with the household, so that the first version does not require individual profiles.
5. As a household viewer, I want to sign out of the Household Account, so that another person using my browser cannot continue performing authenticated actions.

### 媒体目录与扫描

6. As a library maintainer, I want to add a Media Directory that the server can access, so that existing videos can enter the Media Library without being uploaded.
7. As a deployer, I want to mount a NAS share on the host and expose it to the application, so that the application does not need to manage network-share credentials or protocols.
8. As a library maintainer, I want a newly added Media Directory to be scanned automatically once, so that its files become available without a second setup action.
9. As a library maintainer, I want to trigger another Library Scan manually, so that newly added files can be discovered when I choose.
10. As a library maintainer, I want to see whether a Library Scan is running, completed or failed, so that I know whether my collection has been refreshed.
11. As a library maintainer, I want repeated scans of the same files to preserve their existing records and Watch Progress, so that refreshing the library does not create duplicates or reset viewing.
12. As a library maintainer, I want an unavailable mount or failed scan to produce an error rather than empty the Media Library, so that a storage outage is not mistaken for deliberate removal.

### 内容组织

13. As a household viewer, I want to browse a poster wall, so that I can recognize media visually rather than only through filenames.
14. As a household viewer, I want Movies and Series to be categorized separately, so that I can navigate each type appropriately.
15. As a household viewer, I want to see a Movie's title, year, synopsis and poster when available, so that I can decide what to watch.
16. As a household viewer, I want to navigate a Series through its Seasons and Episodes, so that I can find a particular Episode.
17. As a library maintainer, I want Episode numbers to be recognized from filename markers such as S01E01, so that conventionally named Series can be organized.
18. As a library maintainer, I want one Media File to represent one Movie or one Episode, so that playback and deletion have an unambiguous file target.
19. As a household viewer, I want separate versions of the same title to remain separate Media Files, so that the first version does not silently merge different files.
20. As a household viewer, I want unrecognized files to remain visible as Unmatched Media, so that imperfect filenames do not make playable content disappear.

### 影片资料与匹配

21. As a deployer, I want to configure my own TMDB API credentials, so that online Title Metadata can be obtained without embedding another person's secret in the application.
22. As a household viewer, I want Title Metadata and posters to be obtained from TMDB, so that the Media Library is more useful than a raw directory listing.
23. As a household viewer, I want Chinese Title Metadata to be preferred when available, so that the library is comfortable for my household to browse.
24. As a household viewer, I want media to remain accessible through filenames and placeholder images when TMDB credentials are absent, so that initial use does not depend on online setup.
25. As a household viewer, I want a TMDB network failure to leave local playback usable, so that an external service outage does not prevent watching my files.
26. As a household viewer, I want an unsuccessful match to fall back to the filename rather than an arbitrary title, so that incorrect metadata is not presented as certain.
27. As a library maintainer, I want to select a correct match manually when automatic Metadata Matching is wrong, so that I can repair the displayed Title Metadata without renaming the video.

### 浏览器播放与字幕

28. As a household viewer, I want supported Media Files to play directly in the browser, so that the server does not need to transcode them.
29. As a household viewer, I want to seek within a playing Media File, so that I can move to the position I want to watch.
30. As a household viewer, I want a clear incompatibility message and an External Player option when browser playback fails, so that I have a practical fallback.
31. As a household viewer, I want browser playback to save Watch Progress, so that I can leave and continue later.
32. As a household viewer, I want to resume from the household's saved Watch Progress, so that I do not have to find my previous position manually.
33. As a household viewer, I want a matching UTF-8 SRT Sidecar Subtitle to be usable in the browser, so that existing text subtitles can accompany the video without video transcoding.
34. As a household viewer, I want a matching UTF-8 VTT Sidecar Subtitle to be usable in the browser, so that browser-compatible subtitle files work directly.
35. As a household viewer, I want to select or disable available Sidecar Subtitles, so that I can choose whether and which subtitles to display.

### 外部播放器串流

36. As a household viewer using macOS, I want to open a Media File in IINA from the website when the environment supports it, so that I can use my installed player's decoding capabilities.
37. As a household viewer using Windows, I want to open a Media File in PotPlayer from the website when the environment supports it, so that I can use my installed player.
38. As a household viewer, I want a copyable Stream URL to remain available, so that I can paste it into an External Player when automatic launch is unavailable.
39. As a household viewer, I want an External Player to stream the original Media File without waiting for a complete download, so that playback can begin promptly.
40. As a household viewer, I want the Stream URL to work without the External Player inheriting browser cookies, so that the handoff can work between independent applications.
41. As a library maintainer, I want a Stream Credential to authorize only reading one specified Media File, so that possessing a playback link does not grant management or deletion rights.
42. As a household viewer, I want Stream URLs not to expire automatically, so that the first version avoids expiry-related setup and renewal behavior.
43. As a household viewer, I want to understand that anyone holding a Stream URL may read its file and that the link does not expire automatically, so that I do not treat it as a safe public-sharing mechanism.
44. As a household viewer, I want the website not to imply that it has synchronized External Player progress or watched state, so that I understand why external viewing does not update website Watch Progress.
45. As an External Player user, I want the subtitle boundary to be explicit, so that I know to load Sidecar Subtitles myself rather than expect the website to transfer them automatically.

### 删除源文件

46. As a library maintainer, I want Delete Source File to remove the actual video from disk or NAS, so that deleting is not merely hiding a Media Library record.
47. As a library maintainer, I want to see the target file path before deletion, so that I can verify exactly which file will be affected.
48. As a library maintainer, I want a second confirmation and the ability to cancel before deletion, so that an accidental first click does not destroy a video.
49. As a library maintainer, I want an explicit warning that the application cannot recover a permanently deleted file, so that I understand the consequence before confirming.
50. As a library maintainer, I want deletion to affect only one selected Media File, so that an entire Season, Series or directory is not removed accidentally.
51. As a library maintainer, I want associated subtitles, posters, other videos and directories to remain untouched, so that deleting one video does not remove surrounding files.
52. As a library maintainer, I want a failed deletion to report the actual error and retain the undeleted file's record, so that the website does not falsely indicate success.
53. As a library maintainer, I want deletion to be constrained to the intended media location and file, so that a manipulated request cannot remove unrelated server files.
54. As a household viewer, I want the Media Library to reflect the real outcome of a confirmed deletion, so that it does not continue presenting a removed file as available.

### 部署与运维边界

55. As a deployer, I want to run the system on one host using Docker, so that I do not need a distributed media platform.
56. As a deployer, I want mount and permission requirements to be documented, so that playback and explicitly enabled source deletion can access the intended directories.
57. As a deployer, I want application data to persist independently of the media files, so that a normal restart does not discard the library or Watch Progress.
58. As a deployer, I want configuration and usage instructions for the Household Account, TMDB and External Players, so that I can operate the core workflow without hidden setup steps.
59. As a deployer, I want tested environments and unverified compatibility cases to be reported separately, so that generating a player link is not mistaken for successful end-to-end playback.
60. As a deployer, I want the product to remain limited to the agreed core capabilities, so that optional ecosystem features do not increase the first version's operating cost.

## Implementation Decisions

### 已确认的产品与架构约束

1. **单上下文、单机部署。** 采用一个家庭媒体领域，单机 Docker 运行，通过宿主机已挂载的媒体目录访问本地或 NAS 文件。支持删除的目录需要相应写权限。应用不管理 SMB/NFS、片源上传、下载、改名或移动。
2. **一个家庭共享账号。** 网页使用账号密码登录，媒体库与观看进度由家庭共用；不建立个人档案或权限层级。网站管理操作不能被外部串流凭证替代授权。
3. **扫描与资料获取分离。** 添加目录后自动扫描一次，后续手动扫描；不实时监听、不定时扫描。TMDB 凭据或服务不可用不得成为发现文件、展示未匹配媒体或尝试播放的前置条件。
4. **电影和电视剧的内容模型。** 电视剧包含季和单集，通过文件名中的季集编号识别；一个媒体文件对应一部电影或一个单集。不同版本不合并，多集合一文件不在范围内。
5. **影片资料只接 TMDB。** 部署者配置自己的凭据，中文优先；无凭据、请求失败或无法匹配时使用文件名和占位图，允许手动重新匹配。不导入或写回本地 NFO。
6. **浏览器直放与外部播放器并列。** 不做服务器视频转码。对不支持的媒体明确提示；外部播放器目标为 IINA 与 PotPlayer，在兼容环境提供唤起入口，并始终提供复制串流地址。
7. **长期单文件串流授权。** 串流凭证不可猜测、只读、仅对应指定媒体文件、不自动过期，不授予管理或删除权限。用户已接受持有链接者可读且链接泄露后不会自动失效的代价；这不是公网分享功能。
8. **观看进度来源限定。** 网站只记录浏览器内的家庭共用观看进度，不同步外部播放器的观看进度或已看状态，也不承诺外部播放器自身保存进度的行为。
9. **字幕限定为外挂文本字幕。** 网页支持匹配视频文件名的同目录 UTF-8 SRT/VTT，提供选择和关闭。SRT 转为浏览器可用的字幕文本，不改变视频或音频编码；不提取内嵌字幕、不实现 ASS/SSA 特效、不烧录字幕。外部播放器的外挂字幕由使用者自行加载。
10. **单文件永久删除。** 操作前展示源文件路径并二次确认，明确提示“本系统无法恢复”。仅删除选中的视频文件，不批量删除、不删除文件夹、不连带删除字幕或海报，不做回收站。该约束与已有三项决策记录一致，不被后续实现便利性覆盖。

### 实现组织、数据与外部契约建议

以下是从讨论收敛出的实施建议，而非声称用户逐项确认了代码结构或数据库选型；执行者可调整内部组织，但不能改变上述产品行为。

- **技术候选。** 此前建议 Go 后端、React 与 TypeScript 前端、SQLite 持久化、单应用容器及 Docker Compose。它们是推荐默认方案，不是已接受的语言或框架锁定；不因为本次发布规格而新增相应已接受 ADR。
- **能力边界。** 需要覆盖媒体目录与扫描、媒体库与匹配、播放与字幕、观看进度、访问控制、源文件删除和持久化。它们可在同一个应用内协作，不要求拆成微服务；对外以运行中的应用行为作为统一契约。
- **逻辑数据。** 持久化媒体目录、媒体文件身份与可用性、电影或电视剧/季/单集关联、资料来源及匹配结果、家庭共用观看进度，以及所需配置和授权材料。媒体文件身份不能仅用标题或 TMDB 标识代替；媒体目录、影片资料与实际文件不能混为同一实体。
- **重复扫描。** 对仍然相同的媒体文件重复扫描不重复收录，也不无故丢失已有匹配或进度。目录不可访问或扫描失败时不批量清库；成功扫描发现缺失文件时可以保留记录并标为不可用，而不是猜测用户要求删除。
- **外部 API 能力。** 暴露登录与退出、添加目录与触发扫描、查询媒体与季集、匹配候选及确认匹配、读取与更新观看进度、取得字幕、取得外部串流地址、二次确认后的单文件删除等行为；具体路由和内部模块形式由实现确定。
- **媒体读取契约。** 原始文件串流支持 HTTP GET、HEAD 和 Range，正确表达部分内容、无效范围和不可用文件；采用按需读取，不要求先把大文件装入内存。串流地址使用观看设备可访问的网络地址，而不是服务器本机地址或源文件路径。
- **文件访问保护。** 浏览器身份与串流只读授权分别校验；客户端提交的文件标识不能成为任意服务器路径访问能力。读取和删除需要约束到配置的媒体目录，防止路径越界、符号链接逃逸、目标被替换或删除了非视频对象。
- **删除与索引一致性。** 未删除实际文件时不得先宣称成功或抹去记录。若文件删除成功而后续索引更新失败，必须报告实际结果并允许后续扫描校正。测试及演示仅使用专门创建的临时文件。
- **外部依赖。** 不假设外部播放器继承浏览器 Cookie，也不把浏览器协议唤起当作可可靠检测的成功事件。TMDB 请求需遵守署名、限流及缓存期限；普通资料失败不能升级为本地播放失败。
- **界面建议。** 可采用登录、海报墙、详情与季集、网页播放器、媒体目录管理这组最小页面，辅以基础搜索和继续观看入口。精确页面数量、布局、扩展名完整清单、进度保存间隔及重命名识别策略属于此前实现建议，不作为额外的用户确认门槛或固定验收数字。

## Testing Decisions

### 一个主要测试切面：运行中的应用对外边界

本仓库还没有业务代码、现有测试或可复用的测试切面。建议新建一个应用级测试入口：以隔离配置启动真实应用，通过公开 HTTP 行为驱动它，并通过响应、网页可见状态和测试媒体目录中的实际文件结果验收。

- 使用真实的临时媒体目录及临时持久化存储；从正常的目录接入和扫描操作建立测试数据。
- 仅替换系统外部的 TMDB 服务为可控制的测试服务，以模拟成功、错误匹配、超时、限流和失败；不模拟内部扫描、授权、删除或数据库函数来“证明”它们正常。
- 少量浏览器测试仍驱动同一运行中的应用，覆盖页面交互和真实授权链路；不为每个内部模块另造测试入口。
- IINA/PotPlayer 的操作系统唤起及真实播放属于同一用户流程的人工兼容性验收，不把协议字符串单测当作端到端播放验证。

这一本次整理出的切面建议沿用此前的集成验证方向，但没有被用户单独逐项确认；本次按直接成稿要求记录建议，不虚构确认，也不以再次采访阻塞规格发布。

### 什么是好的测试

验证用户或外部客户端可观察的行为及必要的文件副作用，而不是私有函数调用、内部对象结构、具体 SQL、模块命名或固定调用次数。内部重构后行为不变，测试应继续成立。异步扫描通过可观察的完成状态等待，避免依赖随意的固定延时；所有失败断言必须检查真实结果，不能只检查返回了一个成功状态码。

### 必须覆盖的行为

1. **登录与授权：** 未登录不能浏览受保护内容或执行管理操作；错误凭证被拒绝；有效串流凭证仅能读取对应文件，不能读取其他文件、访问管理功能或删除。
2. **扫描与目录：** 新增目录自动扫描、手动发现新文件、重复扫描不重复收录或重置进度；权限不足、挂载丢失和扫描中断有明确反馈，不触发误清库。
3. **内容组织：** 电影与电视剧分类、季集编号识别、一文件一部电影或一个单集、不同版本不合并；识别失败保留未匹配媒体。
4. **TMDB 与降级：** 中文优先、正确匹配、手动修正；无凭据、超时、服务错误、限流、无匹配以及缺少中文字段时仍能浏览文件和使用播放入口。
5. **浏览器播放与串流：** 原始内容响应、HEAD、有效及无效 Range、定位播放、不可用文件；不兼容媒体给出明确提示及外部播放器入口。只返回指定字节范围，不能用整文件缓冲伪装串流。
6. **观看进度：** 浏览器保存并恢复家庭共用进度；重复扫描或正常重启不丢失已持久化进度；外部播放入口不产生虚构的进度或已看状态更新。
7. **外挂字幕：** UTF-8 SRT 与 VTT、选择与关闭、不同换行及字幕格式错误、非支持编码的明确反馈；转换不改写源视频或源字幕，不声称支持内嵌字幕或 ASS 特效。
8. **外部播放器：** 正确处理带空格、中文和保留字符的媒体地址；兼容环境唤起及复制兜底；播放器可独立读取对应媒体。链接不施加自动过期机制，持有它也不能进行管理或删除。
9. **删除：** 展示正确目标、二次确认、取消不删除、单个源文件确实删除；关联字幕、图片、其他视频及目录全部保留。验证权限错误、文件缺失、路径越界、符号链接和目标变化，不得误报成功或操作范围之外的文件。
10. **持久化与部署：** 使用隔离数据验证重启后媒体、匹配及进度的可用性；在具备 Docker 环境时验证容器启动、应用数据持久化与媒体挂载权限。
11. **可见主流程：** 登录 → 添加目录 → 扫描 → 海报墙或未匹配媒体 → 详情/单集 → 播放及续播/字幕 → 外部播放器入口 → 取消或确认单文件删除。

### 现有实践与验证限制

没有可沿用的业务测试范例，不应虚构既有框架、测试套件或已通过结果。可参考项目既定术语和三项 ADR 的外部行为约束。

此前环境探查发现本机有 Go 和 Node 开发工具，但当前 PATH 未找到 Docker CLI；也没有已完成的 Windows/PotPlayer 实机验收。自动测试、真实 TMDB 联通、容器运行和播放器实机验证应分别报告，缺少环境的项目标为未验证，不能以模拟结果代替。

## Out of Scope

- 完整复刻 Emby、多用户、个人观看档案和权限分级。
- 公网分享平台、投屏、直播、电视或原生移动端客户端。
- 服务器视频转码、字幕烧录、内嵌字幕提取、ASS/SSA 特效及所有媒体格式的兼容保证。
- 外部播放器的观看进度或已看状态同步，以及自动传递或发现服务器上的外挂字幕。
- 串流链接自动过期；也不新增专门的链接生命周期管理产品。
- 片源上传或下载、下载器、SMB/NFS 连接与挂载管理、媒体文件改名或移动。
- 实时文件监听、定时扫描、多集合一文件支持、多版本合并。
- 本地 NFO 导入或写回，以及除 TMDB 外的在线影片资料供应方。
- 回收站、应用内恢复、批量删除、整季或整目录删除、连带删除字幕或海报。
- 以“仅移出媒体库”替代删除源文件；本次不额外要求独立的隐藏或移除记录功能。
- 收藏、推荐算法、播放统计和其他未讨论的扩展功能，不得因“类似 Emby”自行加入。

## Further Notes

### 发布与实施状态

本规格按本次 to-spec 请求发布到本地 issue tracker，状态为 ready-for-agent，不再开启采访或额外分诊。该状态表示规格可供后续执行者接手，不表示本轮已经开始编码或已经验证全部运行环境。

此前 Go、React/TypeScript、SQLite、Docker Compose、页面分组及精细工程默认值仅作为建议提出，用户随后要求直接整理规格；本次不将它们伪装成已经接受的技术 ADR。技术建议可调整，已确认的功能边界和现有 ADR 不可被静默覆盖。

### 文档依据与优先级

- [领域术语表](../../CONTEXT.md)
- [ADR 0001：源文件删除限定为单文件永久删除](../../docs/adr/0001-single-file-permanent-deletion.md)
- [ADR 0002：以宿主机挂载目录作为媒体接入边界](../../docs/adr/0002-host-mounted-media-directories.md)
- [ADR 0003：外部播放器使用不过期的单文件只读链接](../../docs/adr/0003-non-expiring-read-only-stream-links.md)
- [前期讨论与来源记录](./discussion-notes.md)
- [实现建议参考](./implementation-plan.md)

本规格与已接受 ADR 为执行依据；历史讨论中的候选方案和实现建议不覆盖它们。本次没有发现与既有 ADR 的冲突。

### 已核实的外部约束

- IINA 有官方 URL 打开协议；目标兼容性应以修复相关 URL 处理漏洞的 1.4.4 或更新版为基础。[官方处理源码](https://github.com/iina/iina/blob/v1.4.4/iina/AppDelegate.swift)、[安全公告](https://github.com/iina/iina/security/advisories/GHSA-w5xh-98j7-jp5q)。
- PotPlayer 官方资料确认 HTTP 播放能力，但未核实各版本及安装方式都自动注册网页唤起协议，不能保证所有 Windows 安装免配置一键打开。[官方更新记录](https://t1.kakaocdn.net/potplayer/PotPlayer/v4/Update2/UpdateEng.html)。
- TMDB 需要应用凭据，不保证所有作品都有中文资料或目标家庭网络一定可访问。非商业使用仍需署名，正式条款限制所获信息缓存不得超过六个月，不得承诺永久离线保存其资料。[应用认证](https://developer.themoviedb.org/docs/authentication-application)、[语言机制](https://developer.themoviedb.org/docs/languages)、[正式条款](https://www.themoviedb.org/documentation/api/terms-of-use)。
- 浏览器外挂字幕以 WebVTT 为基线；SRT 需要文本转换，不能仅更改扩展名。这不要求视频转码。[HTML track 标准](https://html.spec.whatwg.org/multipage/media.html#the-track-element)、[WebVTT 标准](https://www.w3.org/TR/webvtt1/)。

没有约定片库规模、并发人数、吞吐量或延迟指标，也没有性能基准结果；不得自行声称达到某项数值。源文件删除的设计确认不授权对用户当前真实媒体执行演示删除，测试始终使用隔离临时数据。
