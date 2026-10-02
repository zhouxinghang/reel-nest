# 01：契约、数据模型与验证基线

Status: resolved
Blocked by: none

Parent: [全量重构技术方案](../spec.md)

## 目标

将已通过方案落实为可验证的新 HTTP 契约、单一新 schema 和回归基线，为后续 module 提供稳定 interface。本任务不提前替换当前运行实例。

## 范围

- 根据 spec 第 5、8 节编写 `api/openapi.yaml`，明确请求/响应 DTO、枚举、分页、时间、错误和版本字段，覆盖 R01–R12。
- 配置 TypeScript 契约类型生成与一致性检查，生成产物与手写 UI 类型分离；不得仅添加文档而没有自动检查。
- 落实 `internal/store` 的单一新 schema、初始化/版本判定及真实 SQLite 测试；稳定外部身份与可过期内容分开，真实外键/CHECK 约束就位。
- 提供可复用的隔离样本生成和查询基准工具；逐组标记现有测试的保留、按契约改写、纯兼容删除范围。
- 保存旧 API→新 API 的行为差异清单，避免把不兼容误当成可以删除产品能力。

## Acceptance criteria

- [x] 新 schema 能在空临时目录初始化，并在正常重启保持状态；非空旧库/不支持 schema 拒绝打开，不改写旧数据，不自动生成替代密钥。
- [x] 文件、电视剧、资料目标、绑定、快照、演职员、图片和任务的关系符合 spec；目标清理与内容到期不会产生无约束绑定或误删手动选择。
- [x] 新契约覆盖全部 interface；ID 字符串、Unix 毫秒/null、稳定错误码与删除部分成功语义明确。
- [x] 生成 TypeScript 类型可重复，生成后无漂移；请求校验及响应契约测试可执行。
- [x] 现有安全/正常重启/真实播放回归不因“历史兼容”被误删；测试分类可逐项追踪至 R 编号。
- [x] 合成样本覆盖 1,000/10,000/50,000 文件及 5,000 资料目标，可记录查询计划、响应大小与耗时，不操作真实媒体。
- [x] 新增代码和测试通过实际构建及相关测试，结果写入 Answer；未测量的目标不写成已达标。

## 验证与交接

运行 schema 集成测试、契约生成检查、TypeScript/Go 构建及相关回归。向任务 02–05 交付明确的 DTO、schema、错误语义和测试入口。完成本任务不代表新版功能已交付，也不宣称全部性能达标。

## Comments

- 2026-10-02：认领 01；基线 `4dffbaa`，工作区已有本重构方案与任务文档（尚未跟踪），无既有代码改动。仅实现隔离的新契约/store/测试工具，不接入现行 app、不读取真实凭据或媒体、不迁移或切换 DATA_DIR。


## Answer

2026-10-02：本任务完成。仅交付新版基础，**不代表新运行路径/全部重构已完成**；旧应用未切换，02–06 继续按依赖实施。

### 交付

1. `api/openapi.yaml` 固定 R01–R12、39 个 operation，定义显式轻量/详情 DTO、字符串 ID、毫秒/null、分页/cursor、稳定错误码、会话/串流隔离及删除部分成功。`api.Contract` 与逐 operation 样本执行请求/响应校验；它不是鉴权实现，业务/CAS/文件安全由后续 module 验证。
2. 锁定 openapi-typescript；`contracts:generate` / `contracts:check` 支持无漂移生成，生成类型与手写 UI 分离；build/test 已接入检查。
3. `internal/store` 单一 STRICT schema、真实 FK/CHECK、稳定外部身份与快照分离、受约束目标/选择/演职员/图片/任务、读投影和必要索引。只暴露 Open/Close/Keys，不向业务导出 SQL/Tx。使用一写四读，连接级 FK/超时、WAL、0600 独立密钥与 DATA_DIR flock。
4. 空目录初始化与正常重启、真实子进程崩溃/WAL 恢复、旧/未知 schema 拒绝、丢失/替换密钥拒绝、无改写旧目录均由真实 SQLite 测试验证。修复只读 SQLite 仍创建 WAL/SHM 的副作用：干净库 immutable 检查，脏 WAL 在隔离副本检查后才允许原库写连接。
5. [交接与新旧差异](../../../docs/architecture/foundation.md) 固定后续实现责任；[回归清单](../../../docs/architecture/regression-inventory.json) 覆盖97个原测试声明，逐项关联 R 编号，87按契约改写/5拆分保留/4仅兼容退役候选/1进程 harness。本轮**未删除任何旧测试或旧运行代码**。
6. 复用的隔离样本与查询探针覆盖10库、1,000/10,000/50,000文件及最多5,000真实资料目标，每目标100演员+50导演。[测量报告](../../../docs/architecture/schema-baseline.json) 保存 SQL、EXPLAIN、返回行数、行 JSON 字节、首次读取、30次热查询分位与环境。

### 实际验证

环境：macOS arm64，Apple M5 Pro / 18 CPU / 48 GiB，Go 1.25.12、Node 26.7.0、npm 11.19.0，本机 Chrome。仅临时文件/数据库和受控三源 fixture。

| 命令 | 结果 |
| --- | --- |
| `npm run contracts:check` | 通过，生成精确一致，39个operation的样本及非法输入/响应漂移测试通过 |
| `npm run baseline:check` | 通过，97个旧测试声明仍在且均有分类/R追踪 |
| `go test ./api ./internal/store -count=1` | 通过，真实schema约束/回滚/重启/旧库无写入检查通过 |
| `go test ./internal/store -run TestSynthetic -count=1 -args -baseline-output="$PWD/docs/architecture/schema-baseline.json"` | 通过，三档规模全部生成和测量；最终运行43.708秒 |
| `npm test` | 通过，TypeScript/Vite + Go测试 + 27个Playwright场景（1.8分钟）；其中未改动旧Go包命中缓存，另以下一行做不使用缓存的全量验证 |
| `go test -race -count=1 ./...` | 全量通过，无race；旧app包432.410秒、store包6.036秒 |
| `go test -race -count=1 ./api ./internal/store` | 最后测试工具补充后再次通过 |
| `go vet ./...`、`go build ./...` | 通过 |
| `GOOS=linux GOARCH=amd64 go build ./...` | 交叉编译通过，不等于Linux/容器实跑 |
| `npm run format:check`、`gofmt -l api/*.go internal/store/*.go` | 通过/无输出 |
| `npm audit`、`git diff --check`、`git diff --cached --check` | 0漏洞；空白检查通过；新任务/方案文档链接检查通过 |

50,000文件档SQL探针热p95（毫秒）：定向详情0.034、单库首50条3.400、人物50作品10.239、子串搜索0.704。测量与Go race并行，非空闲专用机；这是**SQL探针/行JSON，不是最终HTTP/冷IO/实际访问行数**。不据此宣称spec的200ms/256KiB或完整性能门槛已满足；04须使用生产查询补齐测量。

### 未验证与后续

- 新schema/契约尚未接入现行app；身份/安全文件/扫描提交属于02，资料条件提交/加密与图片属于03，生产查询属于04，新前端/播放/删除属于05，旧代码清理与最终交付属于06。
- 新增分页将季摘要/字幕列表也保持有界；单集页按独立文件版本逐行分页，避免嵌套无界数组。细节已写入契约和交接文档。
- Docker CLI不可用，未做容器实测；真实资料源、NAS、IINA/VLC/PotPlayer及其他编码未验证。不读取真实凭据/媒体，不迁移数据、不重启用户服务、不部署。
- 本提交同时纳入原本未跟踪的已评审方案/审计/六个任务文档；既有ADR、CONTEXT、核心功能issue历史和当前API文档不改写。
- Frontier 更新为02；03–06仍受前置依赖阻塞，未认领。
