# 阶段 05：新前端路由与查询增量

> 历史增量记录：后续已在正式源码入口接入新 UI / runtime；当前实现与测试边界见 [frontend-interactions.md](frontend-interactions.md)。本文当时的“未接入/未切换”描述不代表当前状态，部署和真实数据切换仍未执行。

日期：2026-10-02（Asia/Shanghai）。对应 `.scratch/architecture-refactor/issues/05-frontend-playback-interactions.md`。

## 交付边界

这是 **05 的第二个可验证增量，不是整票完成或正式切换**。新页面位于 `web/src/app/`、`web/src/features/`、`web/src/shared/`，只依赖生成契约和新 runtime。旧 `web/src/main.tsx`、CLI、嵌入的生产 `index.html` 仍保持原入口，不代理或回退到旧 interface。

- `npm run build` 同时检查新旧生产 TS，构建旧嵌入资源，再构建新 UI 到被忽略的 `.reelnest-test/runtime-web/`。
- `tests/runtime-server` 仅在显式传入 `RUNTIME_WEB_DIR` 时提供新 UI；它仍调用真实 `app.NewRuntime`，使用独立临时 SQLite、密钥、媒体目录和家庭会话。默认原生视频 fixture 不受影响。
- `web/runtime.html` / `web/vite.runtime.config.ts` 是分阶段验证入口，不是第二套正式产品或最终兼容方案。05 完整验收后用新入口替代旧入口，06 删除临时并行构建安排和旧实现。
- 没有部署、重启用户实例、迁移/清空现有 DATA_DIR、读取真实凭据或媒体。没有开启真实资料源请求。此轮只改了前端、测试装配、依赖和记录，没有改后端生产实现/契约/schema。

## 已接入的页面

- 家庭登录/退出、401 回登录并携带安全的站内返回 URL；会话切换取消读取及写请求，清空家庭缓存。
- 首页直接使用有界 `/api/home`，包括空媒体库、逐文件继续观看、最近添加，不再先加载整库再在浏览器聚合。
- 分类/搜索/收藏/最近添加/继续观看：React Router 的 URL 保存媒体库、分类、搜索、排序、海报/列表布局、分页。普通单库/最近添加每页 50 张卡；全局分组每页最多 10 组、每组 10 张卡，组内请求与摘要保持相同 limit。两种列表均替换整页，不无限追加 DOM。
- 分组顺序、计数、电视剧聚合、搜索和排序来自服务器。独立 `group.<libraryId>` cursor 不修改其他组位置；变更过滤条件清理全部游标。`CURSOR_STALE` 返回首页，失效的子组起始 token 会重新取得父摘要，不能重复使用同一过期 token。
- 文件只读详情、电视剧季摘要与单集分页、人物及其作品分页读取。季和单集 cursor 保存在 URL；不内嵌全部季集，不在客户端重排作品。
- 三源设置使用新版 PATCH/版本条件提交；后台 Query 更新不会覆盖正在编辑的基线和草稿。发生 `REVISION_CONFLICT` 后禁止直接再次保存，用户显式读取最新版本，保留草稿、展示新版本/地址，再核对保存。秘密字段只在成功保存后清空，不回显已存凭据。
- 共享卡片/收藏入口，以及独立页面、登录、任务状态、设置、卡片 CSS Modules；全局只保留 token/reset。当前是基础视觉与布局，**不是旧页面全部视觉/交互的等价替换**。

文件/系列详情尚无新的播放、手动匹配、外部串流、字幕与源文件删除入口，并有开发预览提示。建库/媒体库管理/扫描 UI、来源排序、显式刷新/失败目标分页及人物资料的真实 UI 验收也尚待完成；不能据此勾选整票复合验收项。

## 请求、缓存及订阅

### 请求

`shared/api.ts` 从生成的 `paths` 推导 method、路径参数、查询参数、JSON body、成功响应类型，不再让调用点任填 `<T>` 或混用旧 POST progress / confirmed 字段。类型反例也纳入测试 TS 门禁。

所有自动查询消费 TanStack Query 的 AbortSignal；默认 15 秒预算覆盖 fetch 和读取响应体。区分 API code/requestId/retryable、超时、网络、非 JSON 响应、主动取消。取消不显示故障，非 JSON 响应不回显代理页面。401 由应用根的 Router 回调处理，无全局事件广播；登录失败不跳转。

客户端自己不重放请求。Query 只对显式 retryable 读取最多重试一次，mutation 一律不自动重试，离线也不排队等待重连后重放。后续删除 UI 仍必须把断连/超时解释为结果未知，**本轮未实现该交互，通用错误分类不能代替删除结果状态机**。

### 缓存

- `features/catalog/queries.ts` 集中稳定 keys 与 query factories；页面类型来自生成 DTO。列表/分页资源 `gcTime: 0`，失去订阅即淘汰，返回时按 URL 重取；其余短缓存最多 60 秒空闲回收。
- 收藏不乐观改按钮：在写前及确认后各取消受影响的在途读取，避免写入期间新开始的旧读取覆盖确认结果；按服务器返回的 Favorite 精确修补所有已缓存卡片。
- 按媒体库/目标限制受影响范围；人物作品、首页及分组缺少单一目标作用域，保守处理其有界投影。仅收藏页需要失效成员资格/事件顺序，普通列表保持 cursor；被取消的首次读取也会恢复，不留下永久 loading。多个卡片实例共享同一目标 mutation busy 状态。
- 手动匹配尚未接入，不能声称匹配竞态已由前端验证。后续同样需要“取消旧读→接受版本化确认→精准失效”的边界，而不是恢复 `metadata-updated`。

### 单一任务订阅

认证壳只挂载一个 `TaskStatus`。运行/排队期间 1.5 秒轮询，空闲或失败 30 秒；隐藏页面禁用轮询并取消在途状态请求，恢复可见时按 stale 规则重新读取。第一次状态读只建立 revision 基线，不额外拉取首页/全库；后续 revision 变化只失效当前订阅的有界投影。

状态 DTO 只给全局 revision，没有变更目标 ID，因此这里不能伪称按具体 subject 精确失效；未订阅的缓存仅标记 stale，不为它们发起全量读取。当前单一订阅负责**资料任务**，扫描任务的 UI/订阅还未接入。

## 验证证据的范围

- `web/src/**/*.test.{ts,tsx}`：快速 Vitest/Testing Library，覆盖请求类型/编码/分类/取消/超时/会话边界、缓存竞态与实际 GC、URL、状态订阅节奏/隐藏/首次读取、设置冲突草稿。
- `tests/runtime/catalog-ui.spec.ts`：真实新 UI + 新 runtime 的 5 个 Playwright 场景，四个临时媒体库（53 个电影文件、12 个短片、空库、54 个单集），验证 URL/前进后退/分页/分组独立 cursor/真实扫描失效/真实收藏/设置 CAS/401/390px/键盘。建库与初始扫描通过真实 API 做 fixture 准备，**不等于建库扫描 UI 已完成**。
- 浏览器 fixture 中的目录记录是合成内容，不用于证明编码播放。原 `tests/runtime/playback.spec.ts` 继续独立验证真实 VP8/Range/字幕/无会话串流，仍不是新产品播放器验收。
- 新增 `@types/node`，将生产浏览器、unit 和新 runtime Playwright 的 TS 配置分开；`npm test` 现在包含 `typecheck:tests` 和 unit。`tests/tsconfig.json` 仅覆盖新 runtime 脚本，不声称历史 Playwright 已全部独立 typecheck。
- 开发中修复了无错误状态误显示通用错误、跨 realm 的 DOMException 取消识别、select 精确可访问名称；修正 fixture 对无来源任务的空闲含义、服务端去后缀标题、macOS `/private/var` 规范路径的错误预期，没有改后端规则让测试通过。

最终命令和结果见 issue 本轮 Progress。桌面/390px 截图存于 Playwright `test-results`（不入库）；实机外部播放器、真实来源/海报、NAS、Docker、完整旧 UI 视觉对照仍未验证。

## 05 下一步及 06 清理线索

1. 继续在这套 Router / Query / generated API 中接入建库/媒体库管理与扫描；不要给缺失功能重新引入旧接口。扫描状态订阅、来源顺序/版本冲突、显式资料刷新/失败目标列表需要新增测试。
2. 完成手动匹配的版本化候选、选择和竞态回归；补真实新 UI 人物/资料/图片联动（目前只有读取组件及既有后端证据）。
3. 完成播放状态机、单在途 + 最新待写快照、最多三秒关闭保存、字幕取消/Blob 生命周期、外部协议编码/复制兜底、两步删除的结果未知/部分成功语义；依赖此前播放后端交接。
4. 补齐旧页面所有视觉/交互，再执行完整新 UI 浏览器链路，才能完成 05、切换装配并解除 06。
5. 06 的可核对清理候选：旧 `web/src/main.tsx` 的手写路由；旧 `api.ts`；`home.tsx`/`library.tsx` 的全库读取/客户端排序；旧全局 `metadata-updated` 和重复轮询；旧全局 `home.css`/`detail.css`/`style.css`。**现在仍被正式旧入口引用，不删除。**待功能全覆盖后才能同时移除旧入口、旧 re-export、旧卡片/样式和分阶段 runtime HTML/构建入口，保留或改写其安全/重启测试，不以新 5 个场景替代全部旧回归。
