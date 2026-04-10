# jcode vs Claude Code 搜索工具对比分析

## 概述

jcode采用单工具设计（纯文本搜索），Claude Code采用四层工具协作架构（Grep + Glob + LSP + ToolSearch）。

---

## jcode 实现分析

### Grep Tool

**文件**: [internal/tools/grep.go](../internal/tools/grep.go) (~300行)

**参数**: Pattern(正则), Path, Include(glob), CaseInsensitive, MaxResults(默认50,最大200)

**执行**: 优先Ripgrep，回退grep
- 排除: .git, node_modules, vendor, __pycache__, .venv
- SSH支持: 通过`env.Exec.Exec()`远程执行

---

## Claude Code 实现分析

### 1. Grep Tool

**文件**: `src/tools/GrepTool/GrepTool.ts`

**扩展参数**:
- `output_mode`: content / files_with_matches / count
- `-B/-A/-C`: 上下文行数
- `type`: Ripgrep文件类型过滤
- `multiline`: 多行匹配模式
- `head_limit` + `offset`: 分页支持

**权限集成**: 应用工具权限规则排除文件
**行长度限制**: 500字符防止minified污染

### 2. Glob Tool

独立的文件查找工具，按名称模式查找，带执行时间指标

### 3. LSP Tool (代码智能核心)

**9种操作**:
- goToDefinition / findReferences
- hover / documentSymbol / workspaceSymbol
- goToImplementation
- prepareCallHierarchy / incomingCalls / outgoingCalls

**特性**: LSP服务器管理、文件打开/变更同步、Gitignore过滤

### 4. ToolSearch Tool

延迟工具发现，支持精确匹配、MCP前缀匹配、关键字搜索

---

## 差异对比表

| 特性 | jcode | Claude Code |
|------|-------|------------|
| **搜索工具数** | 1 | 4 |
| **输出模式** | 仅行列表 | content/files/count |
| **上下文行** | 无 | -A/-B/-C |
| **分页** | 无 | head_limit+offset |
| **多行匹配** | 无 | 支持 |
| **文件类型** | glob | glob + type |
| **LSP集成** | 无 | 9种操作 |
| **权限检查** | 无 | 文件权限规则 |
| **SSH支持** | 有 | 有 |

---

## 改进建议

1. **扩展输出模式** — files_with_matches / count 模式
2. **添加上下文行** — -B/-A/-C 参数
3. **分页支持** — offset + limit
4. **多行匹配** — --multiline 模式
5. **LSP集成** — goToDefinition / findReferences
6. **文件查找** — 独立 glob 工具
