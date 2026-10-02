# 全量重构实施任务地图

- 方案：[已通过的技术方案](./spec.md)
- 审计：[现状与验证基线](./audit.md)
- 评审：2026-10-02，用户确认“评审通过”。
- 当前阶段：01–02 已完成；03 已认领（资料源设置与配置快照增量），04–06 继续等待前置任务。
- 正在推进：[03：资料源、版本提交、人物与图片](./issues/03-metadata-providers-assets.md)（claimed，配置基础增量已实现，整体未完成）。
- Frontier（新的认领）：无；03 尚在处理中，04–06 仍被阻塞。

## 任务与依赖

`ready-for-agent` 表示规格就绪；有 Blocked by 的任务须等前置任务 resolved 才能认领，不表示可以跳过依赖直接实施。

| 编号 | 任务 | Status | Blocked by |
| --- | --- | --- | --- |
| 01 | [契约、数据模型与验证基线](./issues/01-contract-schema-baseline.md) | resolved | 无 |
| 02 | [文件安全、身份与媒体编目](./issues/02-mediafs-identity-catalog.md) | resolved | 01 |
| 03 | [资料源、版本提交、人物与图片](./issues/03-metadata-providers-assets.md) | claimed | 01, 02 |
| 04 | [查询投影、分组分页与性能](./issues/04-catalog-queries-performance.md) | ready-for-agent | 02, 03 |
| 05 | [前端、播放与交互切换](./issues/05-frontend-playback-interactions.md) | ready-for-agent | 03, 04 |
| 06 | [兼容清理、完整验证与交付文档](./issues/06-cleanup-verification-delivery.md) | ready-for-agent | 02, 03, 04, 05 |

## 执行约定

1. 认领前重新检查工作区、AGENTS.md、方案和本任务前置条件；设 `Status: claimed`，同步本地图。
2. 保持各阶段代码可构建、可验证；开发中可逐步替换，但最终必须移除旧运行路径，不交付双栈。
3. 只使用临时媒体/数据库和受控上游；不读取真实凭据、不自动迁移或清空现有数据、不重启用户服务。
4. 完成后在 issue 的 `## Answer` 写明改动、实际验证命令/结果、未验证项，再设 resolved 并更新地图及 Frontier。
5. 部署、切换 DATA_DIR、弃用旧状态是独立操作；评审通过不等于批准执行这些操作。

## Comments

- 2026-10-02：依据已通过 spec 建立任务。仅完成评审记录与任务拆分，未开始代码实施。

- 2026-10-02：01完成契约/类型生成、单一新schema、严格启动检查、三档样本和97项旧回归分类；27个浏览器场景与不使用缓存的全量Go race等验证通过。仅基础交付，未切换应用或部署；详见01 Answer。02解除阻塞。

- 2026-10-02：02 完成安全文件句柄、身份会话、媒体库/组织、分批暂存及集合式扫描发布、每库变更互斥和关闭/重启恢复。新增 23 个测试声明（含进程 helper），全量 Go race、27 个浏览器场景及最终重复回归通过；50,000 行 SQL 发布约 1.851 秒，不是最终 HTTP 性能结果。未切换应用/数据或部署，完整交接见 02 Answer；03 解除阻塞。

- 2026-10-02：03 已推进配置基础增量：加密设置、不可变配置快照、MetaTube 目录空间、设置事务/任务 generation 与新版设置 HTTP 已实现；全量 Go/race、27 个旧浏览器场景及最终专项三轮 race 通过，结果见 03 Progress。adapter/匹配 CAS/人物/图片尚未完成，03 保持 claimed，04–06 不解除阻塞。没有切换或部署。
