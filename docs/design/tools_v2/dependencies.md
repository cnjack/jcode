# 工具系统 V2 依赖库评估

## 依赖库列表

| 功能 | 候选库 | 说明 |
|------|-------|------|
| 文件编码检测 | `golang.org/x/text/encoding` | Go 标准扩展库 |
| MD5 哈希 | `crypto/md5` | 标准库，性能足够 |
| Unified Diff | `github.com/pmezard/go-difflib` | 成熟稳定，Go diff 标准选择 |
| 文件监视 | `github.com/fsnotify/fsnotify` | 跨平台，社区活跃 |
| OAuth2 PKCE | `golang.org/x/oauth2` | Go 标准扩展库 |
| 指数退避 | `github.com/cenkalti/backoff/v4` | 轻量级，广泛使用 |
| 二进制检测 | `net/http.DetectContentType` | 标准库，magic bytes 检测 |
