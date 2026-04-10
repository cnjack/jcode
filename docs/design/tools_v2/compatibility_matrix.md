# 工具系统 V2 接口兼容矩阵

## 工具参数兼容性

| 工具 | V1 参数 | V2 新增参数 | 兼容策略 |
|------|--------|-----------|---------|
| edit | file_path, old_string, new_string, replace_all, start_line, end_line | edits[] | 无 edits 时走 V1 路径 |
| read | file_path, offset, limit | （无新增） | 增加内部检测逻辑 |
| write | file_path, content | （无新增） | 增加冲突检测 |
| grep | pattern, path, include, case_insensitive, max_results | before_context, after_context, context, output_mode, offset, multiline | 新参数全有默认值 |
| ask_user | question, options | questions[], multi_select, description | 无 questions 时走单问题路径 |
| execute | command, timeout, background | description | 内部增强，参数无变化 |
| todowrite | items[] | action, title, blocked_by, id, status | 无 action 时走全量替换 |
