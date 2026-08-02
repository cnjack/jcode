# JCode Artifacts Architecture Review

- 日期：2026-08-01
- 输入：`artifacts-prd.md`、`artifacts-prd-review.md`、`artifacts-design.md`
- 评审者：Grok CLI `grok-4.5`、Kimi CLI 默认 `kimi-code/kimi-for-coding-highspeed`
- 最终门禁：**GO / P0 none**（两者一致）

## 评审过程

首轮 Kimi 认为 metadata/content 线格式、Cloud 状态转换、intent TTL/GC、automation unseen 持久化和 Desktop bridge token 合同不足，结论 NO-GO。补充这些合同后，Kimi 给出 GO。

首轮 Grok 在完整设计上进一步发现 lease takeover 竞态：若新旧 PUT 共享 object key，旧请求晚到可能导致 object body 与 DB digest 分裂。设计随后改为：

- claim CAS 同时绑定随机 `upload_claim_id` 与单调 `upload_generation`；
- 每个 generation 使用不同的 server-only object key；
- uploaded CAS 必须匹配 `state + claim_id + generation`；
- 失败请求只删除自己的 generation object；
- generation 最多 3，GC 可确定性枚举并删除所有代际对象。

同时关闭了四个 P1：Desktop token 从 process/child env scrub、`ciphertext_size = plaintext + 28` 硬合同、`internal/artifact.Service` 作为 Registry 唯一 owner，以及 session-wide viewed 与 per-connection focus 的语义拆分。

## 最终复审

### Grok 4.5

- Verdict：GO
- P0：none
- P1：none
- 五项修正：全部 closed
- 强制测试：lease takeover/late PUT、revoke-in-flight、全代际 GC、Desktop env scrub、wire/AAD vectors、Registry 单 owner、multi-tab unseen/focus。

### Kimi

- Verdict：GO
- P0：none
- 五项修正：全部 closed
- 建议：generation 用尽后由用户 Retry 创建新 intent；所有 Viewer 入口统一触发 viewed PATCH。两项已写回 Technical Design。

## 实现门禁

实现只有在以下条件全部通过后才能合并：

1. Cloud state/store/API/public page 与跨端 crypto vector；
2. JCode Web-only tool、JSONL/Registry/API/UI/Desktop bridge；
3. CLI/TUI/ACP 负面 schema 测试；
4. 真实 Kimi 模型通过 JCode Web 创建并打开 Artifact；
5. Cloud 在 company K8s 使用真实对象存储完成 share/open/revoke 与 plaintext canary 扫描；
6. 两仓对抗性代码审查没有未关闭 P0/P1。
