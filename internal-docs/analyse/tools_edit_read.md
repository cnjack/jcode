# jcode vs Claude Code — Edit/Read/Write 工具深度对比

## 概述

jcode 实现了基础的字符串替换编辑和行范围读取，Claude Code 提供了结构化diff、多媒体支持、冲突检测、LSP集成等企业级能力。

---

## jcode 实现分析

### Edit Tool

**文件**: [internal/tools/edit.go](../internal/tools/edit.go)

- **模式1**: 文件创建 (`old_string=""`)
- **模式2**: 精确字符串替换
- **参数**: FilePath, OldString, NewString, ReplaceAll, StartLine, EndLine
- **容错**: 提示类似行（最长公共子串算法）
- **限制**: 无编码检测、无冲突检测、不支持多编辑

### Read Tool

**文件**: [internal/tools/read.go](../internal/tools/read.go)

- 参数: FilePath, Offset, Limit
- 目录: `ls -la` 命令
- 限制: 无图像/PDF/Notebook支持、无二进制检测

### Write Tool

**文件**: [internal/tools/write.go](../internal/tools/write.go)

- 全文覆写，固定 0644 权限
- 限制: 无冲突检测、无历史备份

---

## Claude Code 实现分析

### FileEditTool

**文件**: `src/tools/FileEditTool/FileEditTool.ts`

**验证阶段** (25+项检查):
- 秘密检查 (防泄露API密钥)
- 文件大小限制 (1GB)
- 编码检测 (UTF-16LE/UTF-8)
- 修改时间冲突检测
- 引号规范化
- settings.json 保护

**执行阶段**:
1. 技能发现 → 2. LSP诊断准备 → 3. 目录创建 → 4. 文件历史备份
5. 补丁生成 → 6. 磁盘写入 → 7. LSP通知 → 8. VSCode通知
9. Git diff → 10. 事件日志

**多编辑支持**: `getPatchForEdits()` 批量编辑同一文件

### FileReadTool

**输出多态**:
- text: 文本+行号
- image: base64编码+尺寸
- notebook: cell解析
- pdf: 全文或分页PNG
- file_unchanged: 重复读去重

**高级功能**: 图像缩放token预算、PDF分页策略、设备文件拦截

### NotebookEditTool

- Jupyter JSON精确解析
- cell级操作: replace/insert/delete
- 保留cell ID、元数据、输出

---

## 差异对比表

| 维度 | jcode | Claude Code |
|------|-------|------------|
| **编辑策略** | 字符串替换 | 结构化diff |
| **多编辑** | 不支持 | 支持 |
| **引号处理** | 仅提示 | 保留排版样式 |
| **编码感知** | 无 | UTF-8/UTF-16 |
| **冲突检测** | 无 | mtime + 内容校验 |
| **文件历史** | 无 | 自动备份 |
| **LSP集成** | 无 | didChange/didSave |
| **图像支持** | 无 | PNG/JPG/GIF/WebP |
| **PDF支持** | 无 | 全文或分页PNG |
| **Notebook** | 无 | 专用工具 |
| **二进制检测** | 无 | hasBinaryExtension |
| **远程能力** | SSH executor | 无 |

---

## 改进建议

1. **实现冲突检测** — mtime + 内容hash
2. **添加结构化diff输出** — unified diff 格式
3. **多编辑支持** — 批量替换同一文件
4. **编码检测** — 自动检测UTF-8/UTF-16
5. **文件大小限制** — 防止OOM
6. **二进制检测** — magic bytes + 扩展名
