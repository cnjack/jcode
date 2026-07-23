# Desktop ↔ Cloud Provider 设计

跨仓实现契约以
[`../cloud/docs/19-account-settings-sync.md`](../../cloud/docs/19-account-settings-sync.md)
为准。

jcode 侧必须维持两个运行通路：

- Desktop Provider：本地保存并直接调用；Cloud 仅持 ASK 加密后的同步信封。
- Cloud Provider：本地只缓存脱敏目录，通过 device-token cloud proxy 调用。

不得为了配置同步把本地 Provider 改成强制 Cloud 代理。未登录 Cloud 时，
现有 provider、模型选择和调用路径必须保持可用。

实现落点：

- `internal/cloud/`：ASK enrollment、provider vault reconcile、Cloud model catalog client。
- `internal/config/`：可移植 provider snapshot 与本地 sync metadata；不改变现有
  `ProviderConfig` 的直接运行语义。
- `internal/model/`：Cloud proxy 虚拟 provider adapter。
- `internal/web/`：同步状态、审批、冲突与 Cloud model API。
- `web/`：Settings → Cloud 配置同步 UI；模型列表按 Desktop/Cloud 来源分组，
  图标继续使用 canonical `kind` registry。

测试清单和发布顺序见 Cloud 契约文档。
