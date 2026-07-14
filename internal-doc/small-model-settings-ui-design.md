# small_model 设置 UI(Web/Desktop)设计

2026-07-15。[small-model-design.md](small-model-design.md) 的 v2 遗留项:
"Web/TUI 设置 UI 尚未露出 small_model(config-file only)"。Desktop 是 Tauri 壳,
UI 由内嵌 Go sidecar 提供的 web 前端渲染,所以做完 web 即覆盖 desktop。
本文记录 API 形状、UI 位置与生效语义的调研结论和拍板建议,实现前先过评审。

## 现状(调研结论)

- 后端 small_model 功能完整(subagent/workflow/team/automation 的 `"small"` 别名、
  LLM 会话标题、doctor 探针),但唯一入口是手编 `~/.jcode/config.json`。
- web 后端没有任何读写 small_model 的端点:`GET /api/config`(internal/web/models.go)
  只返回 provider/model/max_iterations;`POST /api/model` 只切当前任务主模型且不落盘 config。
- 前端 SettingsDialog(web/src/components/SettingsDialog.tsx)没有模型角色概念;
  模型相关 UI 全在 Providers tab(目录、自定义模型、启用开关)。

### 配置热更新的两种既有模式(决定生效语义的关键)

web 模式下存在两条不等价的配置发布路径:

1. **原地写共享指针**(browser.go `handleBrowserConfig`):`s.cfg.Browser = &req` +
   `SaveConfig(s.cfg)`,持 `cfgMu`+`mu`。`s.cfg` 与 command/web.go `buildWebTask`
   闭包捕获的 `cfg` 是**同一个启动指针**,原地写对新旧 engine 全部可见
   (ModelFactory.SmallModelRef 是 call-time 读 `f.cfg.SmallModel`,factory.go:47)。
2. **重载后重指**(providers.go:424 `s.cfg = cfg`):fresh LoadConfig 的新对象只发布到
   server 字段,**buildWebTask 闭包里的旧指针不会更新**——若 small_model 走这条路,
   新建任务的 subagent "small" 别名仍读旧值,直到重启。

small_model 必须走模式 1,否则"保存了但不生效"。

各消费方的生效语义(走模式 1 后):

| 消费方 | 读取时机 | 保存后生效 |
|---|---|---|
| 会话标题 refiner | fire 时 LoadConfig 读盘(command/title.go:34) | 立即,天然生效 |
| subagent/workflow/team 别名 | factory call-time 读 `f.cfg.SmallModel` | 立即(共享指针) |
| automation 别名 | `s.cfg.SmallModel`(automation_run.go:88) | 立即 |

## 拍板建议

### 1. API:专用端点,不做通用 config PATCH

```
GET  /api/config       → 增加 "small_model": "provider/model"(空串=未设置)
POST /api/small-model  → body {"provider":"...","model":"..."};两者皆空 = 清除
```

- body 形状对齐 `POST /api/model` / model-state 系端点({provider, model}),
  空值清除对齐 `SetEffortOverride` 的语义。不引入 `PATCH /api/config`
  (白名单/校验面失控,现阶段只有一个键,scope 不值)。
- 写路径:持 `cfgMu`(+`mu`,照抄 browser.go 的锁序)→ 原地 `s.cfg.SmallModel = ref`
  → `SaveConfig(s.cfg)` → 返回。失败不改内存值。
- 校验:非空时 provider 必须存在于 `cfg.Providers`(400),model 非空即可
  (自定义端点的模型 id 任意;运行期畸形值本就优雅降级)。不做在线探活
  (doctor 已覆盖;"测试连接"按钮列为可选后续)。
- 并发竞态说明:factory 无锁读共享 cfg 字段,与既有 browser 站点权限读取同一模式,
  竞态窗口不新增;如后续要根治应统一 config 读写锁,不在本期。

### 2. UI:Providers tab 顶部 "模型角色" 区块

- 位置:ProvidersTab 最上方、provider 卡片列表之前,新增 "Model roles / 模型角色"
  小节。理由:模型数据(`api.models()` 的 providers+enabled 状态)该 tab 已加载,
  语义归属也对;起名 "roles" 给将来的 memory.model 等留扩展位。
- 控件:一行 "Small model" + 按 provider 分组的下拉(只列 enabled 模型),
  首项 "未设置(跟随主模型)"。选择即保存(与目录启用开关的即时保存一致),
  失败回滚选项并 console.error。
- 帮助文案(i18n,中英):说明两个用途——subagent 等场景的 `"small"` 别名路由、
  LLM 会话标题;未设置时别名降级主模型、标题用截断。
- 前端改动:api.ts 加 `getConfig()` 补 small_model 字段 + `setSmallModel()`;
  modelSlice 加 smallModel 状态;SettingsDialog 加区块;locales 补 key。

### 3. 范围外(记录不做)

- TUI 设置入口(`/model` picker 加 small 角色页)——另行立项。
- extension 内旧 Vue 设置界面(若仍在维护)——React 版是 desktop 的实际 UI。
- memory.model 露出、"测试连接" 按钮、自动推荐 small 模型——观察使用后再议。

## 测试方案

- 后端 handler 单测:设置/清除/provider 不存在/畸形 body;断言 SaveConfig 落盘
  与 `s.cfg` 内存值同步更新;GET 回读一致。
- 生效语义:设置后以 `s.cfg` 构建的 ModelFactory `ResolveRef("small")` 返回新值
  (路由正确性已有 subagent_model_routing_test e2e 锁定,不重复)。
- 前端:dev server 手动全流程——设置→回读→config.json 落盘→新会话标题走小模型;
  暗色/亮色主题下布局检查。

## 实现位置清单

- internal/web/models.go:`handleGetConfig` 补字段;新增 `handleSetSmallModel`
- internal/web/server.go:注册 `POST /api/small-model`
- web/src/lib/api.ts、web/src/app/store(modelSlice)
- web/src/components/SettingsDialog.tsx(ProvidersTab 顶部区块)
- web/src/i18n/locales/*(新 key,中英)
- site/docs/overview/models.md:补一句 "可在设置 → Providers 中配置"
