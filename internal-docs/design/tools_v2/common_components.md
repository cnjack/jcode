# 工具系统 V2 公共组件设计

## 1. Env V2 扩展

```go
// internal/tools/env.go （扩展）

type EnvV2 struct {
    Exec          Executor
    pwd           string
    platform      string
    TodoStore     *TodoStoreV2
    PlanStore     *PlanStore
    FileTracker   *FileTracker
    MCPManager    *MCPManager
    BgManager     *BackgroundManagerV2
    OnEnvChange   func(envLabel string, isLocal bool, err error)
}

func NewEnvV2(pwd, platform, sessionID string) *EnvV2 {
    logDir := filepath.Join(os.UserHomeDir(), ".jcoding", "tasks")
    todoDir := filepath.Join(os.UserHomeDir(), ".jcoding", "todos")
    return &EnvV2{
        Exec:        NewLocalExecutor(platform),
        pwd:         pwd,
        platform:    platform,
        TodoStore:   NewTodoStoreV2(sessionID),
        PlanStore:   NewPlanStore(),
        FileTracker: NewFileTracker(),
        MCPManager:  NewMCPManager(NewFileTokenStore()),
        BgManager:   NewBackgroundManagerV2(nil, logDir), // env 后补
    }
}
```

## 2. 目录结构

```
~/.jcoding/
├── config.json          # 配置文件
├── debug.log            # 调试日志
├── oauth/               # OAuth Token 存储（加密）
│   ├── server1.json
│   └── server2.json
├── todos/               # 任务持久化
│   ├── <session_id>.json
│   └── ...
├── tasks/               # 后台任务输出日志
│   ├── bg_1.log
│   ├── bg_2.log
│   └── ...
└── sessions/            # 会话记录
    └── ...
```
