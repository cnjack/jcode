# Ask User / 用户交互工具对比分析

## 概述

jcode采用极简的单问题+选项模式，Claude Code实现了批量提问、选项预览、多选、语音输入等丰富交互能力。

---

## jcode 实现分析

**文件**: [internal/tools/ask_user.go](../internal/tools/ask_user.go)

- 极简设计: 问题 + 选项数组 → 单一字符串答案
- 基于Go channels的阻塞式异步
- BubbleTea TUI: 底部提示+选项列表+自由文本

### 限制
- 一次只能问一个问题
- 无选项描述或预览
- 不支持多选
- 无撤销/重做
- 无语音输入

---

## Claude Code 实现分析

### 复杂Schema系统

**文件**: `src/tools/AskUserQuestionTool/AskUserQuestionTool.tsx`

```typescript
questionSchema = z.object({
  question: z.string(),
  header: z.string(),            // ≤12字符芯片标签
  options: z.array({
    label: z.string(),
    description: z.string(),     // 权衡说明
    preview: z.string().optional() // markdown/HTML预览
  }).min(2).max(4),
  multiSelect: z.boolean()
})
```

- 支持1-4个问题批量提问
- 用户注解系统
- HTML预览安全验证（防XSS）

### 高级文本输入

**文件**: `src/hooks/useTextInput.ts`

- Emacs-style光标移动
- Kill ring（剪切环）和Yank pop
- 图片粘贴自动base64编码
- Inline ghost text（AI建议）

### 对话系统

**文件**: `src/dialogLaunchers.tsx`

- 11种对话类型
- 动态导入组件（代码拆分）
- Promise-based API

### 语音输入

- Speech-to-text
- Anthropic OAuth认证
- GrowthBook功能门控

---

## 差异对比表

| 维度 | jcode | Claude Code |
|------|-------|------------|
| **提问数量** | 1次 | 1-4次 |
| **选项描述** | 无 | 有 |
| **预览功能** | 无 | Markdown/HTML |
| **多选** | 无 | 支持 |
| **撤销/重做** | 无 | 支持 |
| **语音输入** | 无 | OAuth |
| **图片粘贴** | 无 | 自动编码 |
| **AI建议** | 无 | Ghost text |

---

## 改进建议

1. **批量提问** — 支持多问题一次性收集
2. **选项描述** — 增加description字段
3. **多选模式** — 复选框支持
4. **预览功能** — Markdown渲染预览
5. **历史记录** — 保存上下箭头浏览
