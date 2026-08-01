# Artifact UI 产品事实

更新时间：2026-08-01

本文件只记录 UI 原型可依赖的已验证事实，避免在设计稿中虚构能力。

## 公开产品事实

- JCode 是一个开源 coding agent，面向 Terminal、Desktop 和 Browser 三个 surface。
- Desktop 提供 workspace、automation 和 channel 等产品入口。
- Browser 通过 `jcode web` 启动。
- 三个 surface 共享同一个 agent engine、session 和 tool set；Artifact PRD 进一步把新能力限定在 Web transport，因此 Desktop 通过 Web sidecar 继承，CLI/TUI 和 ACP 不暴露。

来源：[JCode 官方网站](https://www.j-code.net/)

## 仓库实现事实

- Web UI 使用 React 18、Vite、Redux Toolkit，以及 `jcode-ui` / `jcode-ui-core`。
- 当前右栏已有 Plan、Files、Changes 三个 tab，宽度可在 220–600px 之间拖拽。
- Browser 通过 `TopBar` 的 panel menu 打开右栏；Desktop 在 `DesktopTitlebar` 中复用同一入口。
- 主题色来自 `internal/theme/palette.go` 生成的 CSS 变量。默认深色基线为背景 `#111827`、面板 `#1A2333`、主色 `#FF8400`；产品 Web 的基础深色 token 也保持黑灰表面和橙色主动作。
- 产品组件使用 Heroicons outline，不在 React 组件中手写 SVG。
- Desktop 和 Web 共用 `web/` 产品 UI；Tauri 只增加原生文件动作。

来源：本仓库 `AGENTS.md`、`internal/theme/palette.go`、`web/src/App.tsx`、`web/src/components/RightPanel.tsx`、`web/src/components/TopBar.tsx`、`web/src/components/DesktopTitlebar.tsx`。

## Artifact 合同事实

- Artifact 是会话显式登记的工作区文件，不扫描整个 workspace。
- UI 有三层：对话内 tool result card、Artifacts 索引、expanded/fullscreen Viewer。
- 未登录 Cloud 时分享动作完全隐藏；已登录才允许用户显式分享。
- 分享不会启用 session sync，也不会由 `show_artifact` 自动触发。
- Cloud 分享是不可变 revision 快照；工作区新 revision 不会修改旧链接。
- 必须可表达 available、loading、missing、unsupported、too-large、error、uploading、shared、expired、revoked 和 stale-share。

来源：`internal-doc/artifacts-prd.md`。
