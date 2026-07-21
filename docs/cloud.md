# jcode 云端（jcloud）用户指南

把本地 jcode（CLI 或桌面版）登录到 jcode Cloud 后，这台机器就成为一台可被远程查看和控制的"设备"：你可以从 cloud 控制台（浏览器）或手机 app 随时给它派活、看会话、处理审批。本文面向终端用户，覆盖登录、远程使用、配对加密和密钥恢复。

> 命令速查：`jcode cloud guide` 会打印本文的精简版。

## 1. 什么是"设备"

- 一台运行 jcode 并通过 `jcode login` 登录到 jcode Cloud 的机器 = 一台设备。登录信息保存在本机 `~/.jcode/cloud.json`。
- 会话始终在你的机器上运行和保存；云端只做中转（relay）：你的机器主动向云端发起连接，无需开放入站端口、无需 VPN。
- 从控制台或手机发起的会话就是你本机的会话，会出现在本地 sessions 列表里。

## 2. 快速上手

在你要控制的机器上运行：

```bash
jcode login
```

1. CLI 打印一个验证码（user code）和一个确认链接，并尝试自动打开浏览器。
2. 在浏览器里确认这次登录（确认页地址为云端控制台的 `/device` 路由）。
3. 确认后 CLI 自动完成登录，设备随即上线：打开控制台的设备列表（`/devices`）就能看到它。

常用变体：

```bash
jcode login --cloud https://your-cloud.example.com   # self-host 云端（必须 https；仅 localhost/127.0.0.1 开发场景允许 http）
jcode login --name "我的工作站"                        # 自定义设备名（默认用主机名）
jcode login --status                                 # 查看当前登录状态
```

默认云端地址是 `https://cloud.j-code.net`。

登录后，设备随 `jcode web` 启动自动保持连接（可在 `~/.jcode/config.json` 的 `cloud` 块中用 `auto_connect: false` 关闭）。

## 3. 远程使用

打开控制台 **设备列表**（`/devices`）或手机 app 首页：

- 点进一台设备：发起新会话（可带 `plan` / `full_access` 等模式）、浏览历史会话。
- 点进一个会话：实时跟进输出、发消息追加指令、**停止**运行、处理**权限审批**——和在本机上操作一致。
- **设备离线时**：仍可翻看会话历史，但发消息、停止、审批都会被禁用，直到设备重新上线。

## 4. 配对与端到端加密

会话内容**端到端加密**（AES-256-GCM）：云端只存密文和路由元数据，**云端看不到会话内容**。因此每个新客户端（控制台的一个新浏览器、一部手机）必须先与设备"配对"拿到密钥，否则只能看到配对引导卡片。

配对流程：

1. 在新客户端上点击"配对"发起请求（请求 10 分钟有效）。
2. 在设备上查看并批准：

```bash
jcode cloud pairings          # 列出待批准的配对请求（含客户端备注名）
jcode cloud approve <id>      # 批准：把加密后的密钥交给该客户端
jcode cloud deny <id>         # 拒绝
```

批准后客户端自动完成配对，之后即可正常读写加密会话。换浏览器/换手机需要重新配对。

## 5. 密钥与恢复

加密密钥（CEK）在你的第一台设备上生成，永不明文离开你的设备。

- **备份**（强烈建议第一时间做）：

  ```bash
  jcode cloud key show-phrase   # 二次确认后显示 24 词恢复短语
  ```

  恢复短语能解密账号下全部同步内容——请抄写下来离线妥善保存，不要分享给任何人。它不会保存在其他任何地方。

- **恢复**：所有设备都丢失后，在新登录的设备上输入短语重建密钥：

  ```bash
  jcode cloud key recover
  ```

- **换钥**：生成新一代密钥（key_gen+1）；已配对的客户端需重新配对才能读取新内容：

  ```bash
  jcode cloud rotate-key
  ```

## 6. 常用命令

| 命令 | 说明 |
|------|------|
| `jcode login` | 登录 jcloud（设备码流程）。`--cloud <url>` 指定云端（默认 `https://cloud.j-code.net`，self-host 必须 https），`--name` 指定设备名 |
| `jcode login --status` | 显示当前登录状态并退出 |
| `jcode logout` | 退出登录：吊销设备令牌并清除本地凭据 |
| `jcode cloud status` | 显示云端地址、设备 id、密钥代数和连通性（心跳探测） |
| `jcode cloud pairings` | 列出待批准的配对请求 |
| `jcode cloud approve <pairing_id>` | 批准配对（为该客户端包裹密钥） |
| `jcode cloud deny <pairing_id>` | 拒绝配对 |
| `jcode cloud key show-phrase` | 显示 CEK 的 24 词恢复短语 |
| `jcode cloud key recover` | 用恢复短语重建 CEK |
| `jcode cloud rotate-key` | 生成新 CEK（key_gen+1）；已配对客户端需重新配对 |
| `jcode cloud guide` | 打印本指南的精简版 |

**E2EE 开关**：端到端加密默认开启。需要临时关闭（明文上行，用于灰度回滚或排查）时，在 `~/.jcode/config.json` 中设置：

```json
{ "cloud": { "e2ee": false } }
```

然后重启 `jcode web` 生效。

## 7. 常见问题

- **控制台设备列表是空的？** 先在机器上运行 `jcode login` 并完成浏览器确认；控制台须用用户会话登录（服务令牌看不到设备）。
- **新浏览器里会话全是"未配对"提示？** 这是 E2EE 的正常状态——按第 4 节完成配对即可。
- **设备显示离线？** 确认机器上 `jcode web` 在运行且能访问云端；`jcode cloud status` 会做心跳探测并显示连通性。
