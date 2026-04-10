# 工具系统 V2 实现计划

## Phase 1: 安全基础（Edit 核心）

| 任务 | 文件 | 描述 | 依赖 |
|------|------|------|------|
| 1.1 FileTracker 组件 | `internal/tools/file_tracker.go` | mtime + MD5 冲突检测 | 无 |
| 1.2 BinaryDetector 组件 | `internal/tools/binary_detect.go` | 编码检测 + 二进制判断 | 无 |
| 1.3 Read 文件大小限制 | `internal/tools/read.go` | MaxFileSize 检查 | 1.2 |
| 1.4 Edit 集成冲突检测 | `internal/tools/edit.go` | 接入 FileTracker | 1.1 |
| 1.5 Write 集成冲突检测 | `internal/tools/write.go` | 接入 FileTracker | 1.1 |
| 1.6 单元测试 | `internal/tools/*_test.go` | 冲突/编码/二进制测试 | 1.1-1.5 |

## Phase 2: 效率提升

| 任务 | 文件 | 描述 | 依赖 |
|------|------|------|------|
| 2.1 多编辑支持 | `internal/tools/edit.go` | EditInputV2 + applyMultiEdits | Phase 1 |
| 2.2 Unified Diff 输出 | `internal/tools/edit.go` | generateUnifiedDiff | 2.1 |
| 2.3 上下文行参数 | `internal/tools/grep.go` | -B/-A/-C 映射 | 无 |
| 2.4 自适应后台化 | `internal/tools/execute.go` | BlockingBudget + Promote | 无 |
| 2.5 流式进度通道 | `internal/tools/execute.go` | StreamingOutput channel | 2.4 |
| 2.6 TUI 流式渲染 | `internal/tui/tui.go` | 接收 StreamingOutput | 2.5 |

## Phase 3: 持久化

| 任务 | 文件 | 描述 | 依赖 |
|------|------|------|------|
| 3.1 TodoStoreV2 | `internal/tools/todo_store.go` | 磁盘持久化 + 增量 API | 无 |
| 3.2 Todo 依赖关系 | `internal/tools/todo_store.go` | blocked_by 验证逻辑 | 3.1 |
| 3.3 Todo 工具扩展 | `internal/tools/todo.go` | TodoInputV2 多操作路由 | 3.1 |
| 3.4 BgManager 输出持久化 | `internal/tools/background.go` | LogFile 磁盘写入 | 无 |
| 3.5 Stall 检测 | `internal/tools/background.go` | startStallWatcher | 3.4 |
| 3.6 Sleep 检测 | `internal/tools/sleep_detect.go` | 正则匹配 + 告警 | 无 |

## Phase 4: MCP 增强

| 任务 | 文件 | 描述 | 依赖 |
|------|------|------|------|
| 4.1 MCPManager 核心 | `internal/tools/mcp_manager.go` | 连接池 + 状态管理 | 无 |
| 4.2 重连机制 | `internal/tools/mcp_reconnect.go` | 指数退避 | 4.1 |
| 4.3 OAuth 认证 | `internal/tools/mcp_oauth.go` | PKCE + Token 存储 | 4.1 |
| 4.4 资源发现工具 | `internal/tools/mcp_resources.go` | list + read 工具 | 4.1 |
| 4.5 动态连接工具 | `internal/tools/mcp_connect.go` | mcp_connect / mcp_disconnect | 4.1 |
| 4.6 能力检测 | `internal/tools/mcp_manager.go` | Capabilities 解析 | 4.1 |

## Phase 5: 交互增强

| 任务 | 文件 | 描述 | 依赖 |
|------|------|------|------|
| 5.1 选项描述支持 | `internal/tools/ask_user.go` | AskUserOptionV2 | 无 |
| 5.2 批量提问 | `internal/tools/ask_user.go` | AskUserInputV2 多问题 | 5.1 |
| 5.3 多选模式 | `internal/tools/ask_user.go` | multi_select 参数 | 5.1 |
| 5.4 TUI 多选渲染 | `internal/tui/input_views.go` | 复选框组件 | 5.3 |
| 5.5 TUI 批量问题视图 | `internal/tui/input_views.go` | 多问题分步展示 | 5.2 |

## Phase 6: 搜索增强

| 任务 | 文件 | 描述 | 依赖 |
|------|------|------|------|
| 6.1 多输出模式 | `internal/tools/grep.go` | files / count 模式 | 无 |
| 6.2 分页支持 | `internal/tools/grep.go` | offset 参数 | 6.1 |
| 6.3 Glob 工具 | `internal/tools/glob.go` | 独立文件查找 | 无 |
| 6.4 多行匹配 | `internal/tools/grep.go` | --multiline 映射 | 无 |
