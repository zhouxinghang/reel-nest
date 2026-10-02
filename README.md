# Reel Nest

家庭局域网媒体管理：用一个家庭共享账号整理宿主机已挂载的电影、电视剧目录，在网页或外部播放器中观看。Go + React/TypeScript + SQLite；同一进程提供 API 和嵌入式前端。

> **当前版本是不兼容旧应用状态的架构重构。** 首次运行必须使用**新空 `DATA_DIR`**。不导入旧账号、媒体库、资料源设置、媒体 ID、匹配、收藏、观看进度或串流地址。遇到旧 schema、未知文件、不完整初始化或缺失密钥时拒绝启动，不自动清空或修复。正常重启保持本版本状态。代码提交不代表部署；不要直接在旧实例的数据目录启动新程序。

## 能力与边界

- 每个媒体目录对应一个具名媒体库，类型为电影或电视剧；创建后扫描一次，之后手动扫描。空库也有入口，改名只改显示名称。
- 电视剧按季、单集组织，多个文件版本独立播放。支持 `Show.S01E01.webm`、`Show/01.webm`、`Show/Season 2/01.webm`；不确定或多集合一的文件保留为未组织媒体，不自动拆分。不同媒体库的同名作品不合并。
- 首页、分类、子串搜索、收藏、未匹配、最近添加、单库浏览；标题/收录时间排序，海报/列表切换。分类/搜索/收藏按媒体库分组；最近添加全局排序，首页最多 20 项。
- 家庭共享收藏与观看进度相互独立；文件和整部电视剧也可分别收藏。未变化文件重扫不重置身份、收录时间或家庭状态；只有完整成功扫描才清理缺失文件及依赖状态，失败/取消不发布部分索引。
- TMDB、TVDB、MetaTube 影片资料、手动匹配、演职员、人物本地作品与 Primary/Thumb/Backdrop 图片。中文优先、原文回退，资料故障或过期不影响本地文件播放。人物不是外部完整作品年表；不同来源不按名字混合。
- “演员查询”可直接按姓名/别名搜索 MetaTube，查看演员资料、头像与保留比例的壁纸并下载图片，无需先添加影片；同名候选由用户核对，不自动合并本地人物。
- 网页直放原文件、Range 拖动、共享续播、UTF-8 SRT/VTT 外挂字幕；IINA/PotPlayer/VLC 协议入口及复制地址兜底。
- 二次确认后永久删除**单个源视频**，不删除字幕、图片、相邻视频或目录。没有批量删除或回收站。

不提供转码、上传、下载器、文件改名/移动、SMB/NFS 管理、自动定时扫描、多用户或个人进度。媒体扩展名可被扫描不等于浏览器支持其编码/音轨；Windows 服务端不在支持范围，Windows 外部播放器是客户端能力。

## 本地构建与新实例

需要 Go 1.25+、Node 22+、npm。服务端文件安全实现支持 Linux/macOS。

```sh
npm ci
npm run build
mkdir -p bin
go build -trimpath -o bin/reelnest ./cmd/reelnest
```

准备**新目录**与密码文件（密码 12–72 字节；首次账号名 1–80 个 Unicode 字符）。以下仅为操作示例，执行前选择自己的新路径，不复用旧 `data/`：

```sh
umask 077
# 用密码管理器/编辑器创建 /absolute/private/household-password，避免把密码留在命令历史。
DATA_DIR=/absolute/private/reelnest-new \
MEDIA_ROOTS=/absolute/host-mounted/media \
HOUSEHOLD_USERNAME=family \
HOUSEHOLD_PASSWORD_FILE=/absolute/private/household-password \
LISTEN_ADDR=127.0.0.1:8091 \
./bin/reelnest
```

打开 [本地新实例](http://127.0.0.1:8091)。默认 `DATA_DIR=./data`、`LISTEN_ADDR=:8090`，示例故意不使用默认值以避免与旧实例冲突。开发前端 `npm run dev` 默认代理到 8090，使用其他端口时同步调整 Vite proxy。单个 DATA_DIR 只允许一个进程，实例关闭会排空/取消后台工作；不要强行绕过目录锁。

| 参数 | 当前语义 |
| --- | --- |
| `DATA_DIR` | 首次为空；包含 SQLite、`stream.key`、`metadata-settings.key` 及可能的 WAL/SHM。文件不得对 group/world 开放；备份与恢复须包含全部文件 |
| `MEDIA_ROOTS` | 允许接入的宿主机目录，Linux/macOS 用 `:` 分隔，最多 100 个；无值时不能接入媒体库 |
| `HOUSEHOLD_USERNAME` | 首次创建家庭账号；正常重启不覆盖已有账号 |
| `HOUSEHOLD_PASSWORD` / `_FILE` | `_FILE` 优先，移除文件末尾 CR/LF；不可读取则拒绝启动，错误不回显秘密。启动参数不重置已保存账号 |
| `LISTEN_ADDR` | 监听地址；对 LAN 开放时配置主机防火墙 |
| `COOKIE_SECURE` | 仅字符串 `true` 开启；HTTP 局域网保持 false，同源 HTTPS 设置 true |

应用数据与媒体根目录不得相同、互为祖先；媒体库目录不得重叠、越界或经过符号链接。NAS 必须由部署者先挂载；应用不持有共享协议凭据。播放依靠读取权限，删除还需要父目录写/搜索权限。不要对敏感或不可信进程可随意改动的目录开放删除。

## Docker Compose（配置已提供，当前机器未实测）

镜像以 UID/GID `10001:10001` 运行，根文件系统只读，`/data` 持久卷可写，`/tmp` 为 tmpfs，移除 capabilities。Dockerfile 包含生成前端和 Go 嵌入契约/资源。

1. 复制 `.env.example` 为 `.env`，创建 `secrets/household_password.txt`；保护秘密，但保证容器 UID 10001 能读取该文件。
2. 选择**从未使用过的 Compose project 名称**，使 `app_data` 是新空卷；不要重新使用旧项目的卷。
3. 确认 `MEDIA_HOST_PATH` 已存在、已挂载，媒体权限与 UID/GID 相符。

只读媒体（推荐初始部署/切换演练）：

```sh
docker compose -p reelnest-new -f compose.yaml -f compose.media.yaml up -d --build
```

确需永久删除时，**改用**读写 overlay，不要把两份 media overlay 同时传入：

```sh
docker compose -p reelnest-new -f compose.yaml -f compose.media-write.yaml up -d --build
```

省略 media overlay 可启动空实例，但不能浏览宿主机媒体。默认映射 `0.0.0.0:8090`，可设置 `HOST_BIND`/`HOST_PORT`。资料源只在页面配置，旧三份资料源 Compose、旧环境导入和升级脚本已删除。以上是部署说明，不表示本次执行了部署、容器验收或真实删除。

## 使用

### 媒体库与影片资料

1. 登录后进入“媒体库管理”，输入展示名、允许根内路径与类型，按需要勾选/排序资料源。创建并等待扫描终态；之后显式重新扫描核对磁盘变化。
2. 在“资料源设置”配置 TMDB Read Access Token、TVDB API Key/可选 PIN、MetaTube 服务 URL/可选 Token；再为库选择顺序。MetaTube 仅支持电影，允许显式配置的内网 HTTP 服务。
3. 设置 AES-256-GCM 加密保存，密钥独立持久化；页面仅显示是否已配置，**不表示已联网验证**。秘密留空表示保持，勾选清除才删除；更换 MetaTube 地址须提供新 Token 或明确清除旧 Token。旧环境变量不导入、不生效。
4. 自动匹配只使用完整候选中唯一可靠的名称/年份匹配，不跨源拼接。文件查询移除扩展名，`.`/`_` 当空格；可识别末尾 `(YYYY)`。无法确定时使用详情页“匹配影片资料”，搜索并确认目标。
5. 手动选择不会被自动任务覆盖；过期快照不继续当有效资料展示，但选择仍保留。可显式刷新资料；它不扫描文件。可选演职员/图片失败有退避和单独刷新，不丢失已验证的基本资料。
6. 设置或库信息发生并发冲突时保留草稿，重新读取当前版本后再保存；匹配候选过期需重新搜索，不能盲目重复提交。

列表默认每页 50、最大 100；全局分组与每组条目独立分页。页面 URL 保留范围、搜索、排序、视图、季与分页状态；数据变更或资料到期可能使游标失效，客户端重新载入，不承诺跨变更的数据库快照。页面不累计无限 DOM。资料任务全局一处轮询，活跃 1.5 秒、空闲 30 秒、隐藏暂停；扫描监控仅覆盖最近访问管理页的最多 50 项，不是所有库任务总数。

### 播放、字幕、串流

- 网页直接读原文件，不转码或整文件缓存。实际解码失败时改用外部播放器；自动播放被浏览器拦截时手动开始。
- 先恢复续播位置再播放；从头播放忽略旧位置。只有原生 `ended` 才写已完成。约每 10 秒和 pause/seek/隐藏时保存，最多一个在途进度请求加一个最新待写快照。
- 关闭播放器最多等待 3 秒，超时可退出并提示保存未确认。离开页面仅尽力保存，不保证必达；不同浏览器最后收到的有效更新生效。
- 同目录、同视频文件名前缀的 UTF-8 `.srt` / `.vtt` 可选/关闭，服务端转换 SRT，不写回字幕；内嵌字幕与其他编码不保证支持。外部播放器自行加载字幕。
- 外部地址带**不过期、单文件只读凭证**，不依赖 Cookie，不授予字幕、图片、进度或管理权限；外部播放不写网页进度。复制/转发会泄露读取权，不应用于公网分享。没有单链接撤销界面；不要通过手工删除密钥尝试撤销（会使启动失败）。
- `localhost` 地址只能在当前设备使用；跨设备须使用可达 LAN 主机地址。协议链接正确不等于目标播放器已安装、注册或成功播放；IINA/PotPlayer/VLC 实机未验证。

### 永久删除与文件系统风险

先预览准确绝对路径及风险，再勾选二次确认。预览有效期 10 分钟，提交时再次核验文件版本、父目录与媒体库身份；仅处理一个视频。

- `deleted / updated`：视频已永久删除，索引已更新；下一次完整扫描清理记录及依赖。
- `deleted / pending`：视频已删除，但索引待核对。修复存储/重新扫描，**不要再次删除**。
- `unknown` 或网络断连：结果未确认，核查目录再扫描；客户端不自动重试。
- 明确的提交前权限/版本/期限错误：不执行删除，处理原因后重新预览。

**本系统无法恢复源文件。** 备份应用 DB 不能恢复视频。Linux/macOS 的句柄、nofollow、父目录与 inode/ctime/mtime 核对缩小竞态，但 POSIX 没有原子 compare-and-unlink，外部进程仍可能在最终核验与 unlink 之间改动目录。NAS/阻塞内核调用不保证被 context 立即中断；超时/断连不能当成未删除。文件版本基于路径及设备/inode/大小/mtime 等，不做全视频内容哈希。

## 验证与结构

```sh
npm run verify:foundation
# 完整新界面集成复跑（仅临时媒体与数据库）
npx playwright test tests/runtime --repeat-each=3
# 最终 HTTP 性能报告
go test ./internal/store -run '^TestCatalogHTTPPerformance$' -count=1 -timeout 15m -v \
  -args -catalog-http-report="$PWD/docs/architecture/catalog-http-final.json"
go test ./internal/transport/http -run '^TestSlowProviderHTTPIsolation$' -count=1 -timeout 2m -v \
  -args -slow-provider-report="$PWD/docs/architecture/slow-provider-final.json"
REELNEST_LARGE_WALK=1 go test ./internal/mediafs -run '^TestLargeWalkRemainsBounded$' -count=1 -v
go test ./internal/store -run '^TestLargeScanUsesBoundedStagingPages$' -count=1 -v -args -scan-fixture-files=50000
```

浏览器使用 macOS Chrome 或 `CHROME_BIN`，其他环境须提供 Playwright Chromium。测试只操作随机临时目录与受控上游，不能替代真实服务/播放器/NAS/容器验收。测试运行时需要空闲 loopback 端口 18090、18092、18094–18097。不要把测试目录指向用户数据。

- [HTTP 使用说明](docs/api.md)、[OpenAPI 唯一契约](api/openapi.yaml)；类型通过 `npm run contracts:generate` 生成，检查不得手改生成文件。
- `internal/app` 只装配；`catalog` 编目/扫描；`metadata/providers` 仅外部协议；`metadata` 资料流水线；`playback` 文件操作；`mediafs` 安全句柄；`store` 唯一 SQL/事务；`transport/http` DTO/路由。依赖方向由 `npm run architecture:check` 核验。
- `web/src/app` 页面壳/路由，`features` 功能与 CSS Modules，`shared` 请求/UI，`generated` 契约类型；生产及测试使用相同嵌入资源，没有旧 UI 构建或兼容后端。
- [当前交付验证与 R01–R12 证据](docs/architecture/final-delivery.md)、[97 项历史回归追踪](docs/architecture/regression-inventory.json)。历史 [verification](docs/verification.md) 与阶段交接保留为历史，不作为当前入口说明。

## 切换、备份与回退（需另行授权执行）

1. 先记录旧实例二进制/镜像、启动参数、挂载、端口与 DATA_DIR；计划停止旧实例后，完整封存其应用数据和密钥。离线一致备份比单独复制运行中的 `.sqlite` 安全；存在 WAL 时不可遗漏。备份含密钥和 DB，属于敏感数据，限制访问。
2. 新程序只用另一份**新空目录/新命名卷**，重新创建账号、媒体库及资料源设置，重新扫描。新版本会产生新媒体 ID，旧匹配/收藏/进度/串流 URL 不导入，浏览器旧页面与 Cookie 不承诺跨版本兼容。
3. 优先只读媒体挂载，在独立端口演练；前后端同时发布。验证就绪、登录、重启持久化、扫描与读取；不要拿真实视频测试删除。写权限必须另外显式启用。
4. 回退前停止新实例，使用**旧程序 + 封存的旧数据目录**；不让旧程序打开新 DB，不逆向合并新期间进度/收藏。确认挂载/权限/端口后再启动旧实例。
5. 回退只能还原应用状态，不能撤销媒体文件变化，尤其不能恢复永久删除的视频。密钥缺失时恢复完整备份，不生成替代密钥“修复”。不要使用 `docker compose down -v` 或清空旧目录来绕过拒绝启动。

本轮仅代码与隔离验证交付，没有停止旧实例、读取真实凭据、扫描/删除用户媒体、导入状态或切换部署。
