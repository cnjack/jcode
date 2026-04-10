# MCP 集成差异分析：jcode (Go) vs Claude Code (JS/TS)

## 概述

jcode采用最小化MCP集成（仅工具发现），Claude Code实现了完整的MCP协议支持（工具+资源+认证+权限+插件）。

---

## jcode 实现分析

### 配置管理

**文件**: [internal/config/config.go](../internal/config/config.go)

```go
type MCPServer struct {
    Type    string            // "http", "sse", "stdio"
    Command string
    Args    []string
    Env     []string
    URL     string
    Headers map[string]string
}
```

### 工具加载

**文件**: [internal/tools/mcp.go](../internal/tools/mcp.go)

- 3种传输: stdio/SSE/HTTP
- 启动时加载一次
- 无重连机制
- MCP工具与内置工具平等对待

### MCP子命令

**文件**: [cmd/jcode/mcp.go](../cmd/jcode/mcp.go)

- `coding mcp add <name> <url-or-command>`
- `coding mcp list`

### 局限性
- 无资源机制
- 无OAuth认证
- 无运行时动态连接
- 无能力检测

---

## Claude Code 实现分析

### 完整服务层

**7种传输**: stdio, SSE, SSE-IDE, HTTP, WebSocket, SDK, Claude.ai代理

**7层配置作用域**: local, user, project, dynamic, enterprise, claudeai, managed

### OAuth认证

**文件**: `src/services/mcp/auth.ts`

- RFC 9728 + RFC 8414 授权服务器发现
- OAuth2 授权码流程（PKCE）
- 令牌自动刷新
- Keychain安全存储
- 跨应用访问 (XAA)

### 资源发现与读取

- **ListMcpResourcesTool**: LRU缓存，自动重连
- **ReadMcpResourceTool**: 文本/二进制自动处理

### MCP认证工具

- 检测401 → 创建伪工具 → OAuth流程 → 自动重连

### 连接管理

- 指数退避重连 (1s → 30s, 最多5次)
- 会话恢复
- 实时状态同步

### 权限与审批

- 服务器批准对话框
- Channel权限
- 权限缓存持久化

### MCP技能构建

- 从MCP服务器自动包装技能
- 注入系统提示

---

## 差异对比表

| 功能 | jcode | Claude Code |
|------|-------|------------|
| **传输方式** | 3种 | 7种 |
| **配置作用域** | 1(全局) | 7(分层) |
| **工具发现** | 基础 | LRU缓存+实时更新 |
| **资源处理** | 无 | 列表+读取+二进制 |
| **OAuth认证** | 无 | RFC规范+令牌刷新 |
| **重连机制** | 无 | 指数退避 |
| **权限体系** | 无 | Channel+用户交互 |
| **错误处理** | 通用 | 5+专门类型 |
| **插件集成** | 无 | 完整支持 |
| **技能构建** | 无 | MCP→Skill自动转换 |

---

## 改进建议

### P0（核心）
1. **OAuth认证** — RFC标准PKCE流程
2. **资源发现和读取** — resources/list + resources/read
3. **能力检测** — 按capabilities切换功能

### P1（高价值）
4. **动态连接管理** — 运行时添加/移除服务器
5. **重连机制** — 指数退避自动重连
6. **MCP权限** — 服务器级/工具级权限
7. **MCP技能** — 自动生成技能描述
