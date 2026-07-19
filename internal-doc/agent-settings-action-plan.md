# Agent Settings 可视化控制实施清单

> 状态：完成
>
> 创建日期：2026-07-19
>
> 范围：ToolSearch 渐进式工具披露、Project Memory、自动整理（Dream）

## 执行规则

1. 每完成一个编号任务，立即把复选框改为 `[x]`，并补充测试结果。
2. ToolSearch 首版只开放总开关；Direct/Deferred 分类、分组和搜索协议继续由代码策略维护。
3. “Dream Memory”映射到现有 `memory.generate`，不创建第二套记忆系统。
4. 配置接口只返回安全子集，禁止返回 API Key、原始记忆正文、原始 ToolSearch query 或完整工具参数。
5. 破坏性操作只允许明确的项目作用域，并要求 UI 二次确认。

## 总体进度

| 阶段 | 状态 | 说明 |
|---|---|---|
| AS-00 清单与基线 | 完成 | 清单、工具链、干净 Git 基线及相关 Go 测试已记录 |
| AS-01 配置并发安全 | 完成 | 快照发布、串行保存、精确回滚、原子落盘与 pipeline 隔离均已通过 race 测试 |
| AS-02 ToolSearch Settings | 完成 | API、模式/统计 UI、动态统计与全存活 Agent 热刷新均已通过回归及生产构建 |
| AS-03 Memory API | 完成 | metadata 状态、全局配置、异步整理、项目绑定清除及刷新 warning 已完成 |
| AS-04 Memory Settings UI | 完成 | 独立页面、五 locale、状态/预算、基础/高级控制与确认操作已完成 |
| AS-05 验证与收尾 | 完成 | 相关包 race、全量 Go 测试、双端 lint、生产构建与最终并发审计均已通过 |

## AS-00 清单与基线

- [x] AS-00.1 建立任务清单和逐项更新规则。
- [x] AS-00.2 记录 Git、Go、Node、pnpm 基线与工作树状态。
- [x] AS-00.3 运行相关包的修改前基线测试。

## AS-01 配置并发安全

- [x] AS-01.1 为 ToolSearch 和 Memory 提供不可变配置快照/发布入口。
- [x] AS-01.2 Settings 保存使用 `cfgMu` 串行化，并在落盘失败时回滚 live config。
- [x] AS-01.3 Memory pipeline 启动时使用配置快照，避免与 Settings 并发修改竞态。
- [x] AS-01.4 覆盖缺省值、显式 false、保存失败回滚和 race 测试。

## AS-02 ToolSearch Settings

- [x] AS-02.1 新增安全的 ToolSearch 状态/配置 API。
- [x] AS-02.2 保存后刷新全部存活 Web Agent；刷新失败返回可见 warning，不回滚已持久化配置。
- [x] AS-02.3 General → Agent 行为增加 Progressive Tool Disclosure 开关。
- [x] AS-02.4 展示当前模式及 Direct/Deferred/MCP Deferred 只读统计。
- [x] AS-02.5 增加 API、热更新回归测试及前端 typecheck/build smoke。

## AS-03 Memory API

- [x] AS-03.1 新增 Memory status API，返回 resolved config、项目作用域和 metadata-only 状态。
- [x] AS-03.2 新增 Memory config API，并校验所有数值范围和模型引用格式。
- [x] AS-03.3 新增异步 `sync` 操作，返回运行/忙碌状态且不阻塞 HTTP 请求。
- [x] AS-03.4 新增仅限当前项目的 clear 操作，复用 pipeline lock 并在忙碌时返回冲突。
- [x] AS-03.5 覆盖状态、配置、异步运行、busy、clear 和远程工作区边界测试。

## AS-04 Memory Settings UI

- [x] AS-04.1 Settings 新增 Memory 页面，并加入五种 locale。
- [x] AS-04.2 增加 Memory 总开关与“自动整理（Dream）”依赖开关。
- [x] AS-04.3 增加模型、每日预算、冷却、保留窗口、Top-N 和注入预算高级配置。
- [x] AS-04.4 增加今日预算、摘要、Inbox、最近运行与 consolidation 状态卡。
- [x] AS-04.5 增加“立即整理”和“清除此项目记忆”操作及明确反馈/确认。

## AS-05 验证与收尾

- [x] AS-05.1 运行 config/web/memory/command 相关 Go 测试与 race 测试。
- [x] AS-05.2 运行 Web TypeScript typecheck/build。
- [x] AS-05.3 运行 `make lint-go`、`git diff --check` 和最终工作树审计。
- [x] AS-05.4 核对本清单所有完成项的证据并更新最终状态。

## 基线

- Git：`fc3dd996b33ace94aa526800a08e060b4f3cce4d`
- 工作树：干净
- Go：`go1.26.4 darwin/arm64`
- Node：`v26.3.0`
- pnpm：`11.13.0`

## 更新日志

| 时间 | 更新 | 测试 |
|---|---|---|
| 2026-07-19 | 完成 AS-00.1～AS-00.2：创建清单并记录干净 Git/Go/Node/pnpm 基线 | 待运行基线测试 |
| 2026-07-19 | 完成 AS-00.3 与 AS-00：修改前相关 Go 包基线通过 | `go test ./internal/config ./internal/memory/... ./internal/web ./internal/command` |
| 2026-07-19 | 完成 AS-01.1、01.3～01.4：ToolSearch/Memory 不可变配置快照、safe setter、pipeline snapshot 与 `ErrAlreadyRunning` | `go test ./internal/config ./internal/memory/... ./internal/web`；`go test -race ./internal/config ./internal/memory/pipeline` |
| 2026-07-19 | 完成 AS-02.3～02.4、AS-04：ToolSearch General 控件；Memory 独立页、Dream、状态/预算、高级参数、同步/清除与五 locale | `pnpm --dir web typecheck`；`git diff --check` |
| 2026-07-19 | 加固 AS-01.3、AS-03.3～03.4 底层并发：异步启动同步取得 lease、panic 转错误、项目外稳定 pipeline lock、锁失败禁止清理 | `go test -race ./internal/memory/...` |
| 2026-07-19 | 完成 AS-01：Settings 串行保存与精确内存回滚；配置改为 0600 临时文件完整写入、sync、原子替换，失败保留原字节 | `go test -race ./internal/config` |
| 2026-07-19 | 完成 AS-02.1～02.2、AS-03：安全 API、项目绑定操作、异步终态 warning、sync/clear 后 Agent prompt 刷新及并发 rebuild 串行化 | `go test -race ./internal/config ./internal/memory/... ./internal/web` |
| 2026-07-19 | 加固 AS-02/AS-04 交互：显示 Eager/Progressive 当前模式、提交成功与状态刷新失败分离、失败重试、作用域文案与无障碍语义 | `pnpm --dir web typecheck` |
| 2026-07-19 | 完成 AS-05.1，并让 MCP Config RMW/回滚与 ToolSearch/Memory 共用 `cfgMu`；OAuth 使用副本，避免异步 live pointer 竞态 | `go test -race ./internal/config ./internal/memory/... ./internal/web ./internal/command` |
| 2026-07-19 | 完成 AS-02.5、AS-05.2：ToolSearch 动态统计与热更新回归通过，五 locale Settings 完成 TypeScript 与生产构建 | `pnpm --dir web typecheck`；`make build-web` |
| 2026-07-19 | 终审加固：修复 Memory clear/start 锁顺序、MCP 重载乱序与登录快照、approve-all/Agent rebuild 串行化、Setup/Settings 并发保存和 `needsSetup` 读写竞态 | 新增对应回归测试；`go test -race ./internal/config ./internal/memory/... ./internal/web ./internal/command` |
| 2026-07-19 | 完成 AS-05.3～05.4 与全部任务：全量测试、双端 lint、生产构建、diff 及工作树范围审计通过 | `go test ./...`；`make lint-go`；`make lint-web`；`make build-web`；`git diff --check` |
