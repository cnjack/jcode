/**
 * ApprovalBlock — the headless approval gate.
 *
 * Owns: the pending/resolved state split, the decision contracts, the "arming"
 * UX (two-step confirm for blanket approvals to prevent accidents), and
 * dispatching via runtime actions. Does NOT own styling or the tool-name→icon
 * mapping (those live in the styled wrapper).
 *
 * Two decision shapes:
 *  - classic boolean: allow once / allow all (armed) / deny → `resolveApproval`
 *  - host-defined `approval.options` (arbitrary ids, e.g. ACP
 *    permission_request) → `resolveApprovalOption`; if the host didn't provide
 *    that action, non-billable kinds fall back onto the boolean contract so
 *    mock/demo runtimes keep working. billable_external always fails closed.
 *
 * The `resolving` flag on the approval object disables controls while a resolve
 * request is in flight (prevents double-submit).
 */

import { useState } from 'react'
import type { ReactNode } from 'react'
import type { Approval, ApprovalOption } from '../types/index.js'
import { useRuntimeActions } from '../runtime/context.js'

export interface ApprovalDecisionActions {
  allowOnce: () => void
  allowAllArm: () => void
  allowAllConfirm: () => void
  allowAllCancel: () => void
  deny: () => void
  armed: boolean
  /** Whether opaque option ids can be returned without boolean coercion. */
  canResolveOptions: boolean
  /** Options mode: choose a non-arming option (kind ≠ 'allow_always'). */
  choose: (optionId: string) => void
  /** Options mode: id currently armed for two-step confirm, or null. */
  armedOptionId: string | null
  /** Options mode: arm an 'allow_always' option (first click). */
  armOption: (optionId: string) => void
  /** Options mode: confirm the armed option (second click). */
  confirmOption: (optionId: string) => void
  /** Options mode: cancel arming. */
  cancelArm: () => void
}

export interface ApprovalBlockRenderSlots {
  /** Render the pending decision card. Receives the action callbacks. */
  renderPending?: (approval: Approval, actions: ApprovalDecisionActions) => ReactNode
  /** Render the resolved inline note. */
  renderResolved?: (approval: Approval) => ReactNode
}

export interface ApprovalBlockProps extends ApprovalBlockRenderSlots {
  approval: Approval
  /** className passthrough. */
  className?: string
}

/** Map an option kind onto the boolean contract — the fallback used when the
 *  host implements only `resolveApproval`. */
function booleanFallback(
  resolve: (id: string, approved: boolean, approveAll?: boolean) => void,
  approvalId: string,
  option: ApprovalOption,
) {
  const kind = option.kind ?? 'custom'
  if (kind === 'deny') resolve(approvalId, false, false)
  else resolve(approvalId, true, kind === 'allow_always')
}

export function ApprovalBlock({ approval, className, renderPending, renderResolved }: ApprovalBlockProps): ReactNode {
  const actions = useRuntimeActions()
  // Arming state — the user must click twice for blanket approvals (first
  // arms, turning the button cautionary; second confirms).
  const [armed, setArmed] = useState(false)
  const [armedOptionId, setArmedOptionId] = useState<string | null>(null)

  if (approval.resolved) {
    return (
      <div data-jcode-ui="" className={className}>
        {renderResolved?.(approval) ?? <DefaultResolved approval={approval} />}
      </div>
    )
  }

  const allowOnce = () => actions.resolveApproval(approval.id, true, false)
  const allowAllArm = () => setArmed(true)
  const allowAllConfirm = () => actions.resolveApproval(approval.id, true, true)
  const allowAllCancel = () => setArmed(false)
  const deny = () => actions.resolveApproval(approval.id, false, false)

  const dispatchOption = (optionId: string) => {
    const opt = approval.options?.find((o) => o.id === optionId)
    if (!opt) return
    if (actions.resolveApprovalOption) actions.resolveApprovalOption(approval.id, optionId)
    else if (approval.approvalClass !== 'billable_external') {
      booleanFallback(actions.resolveApproval, approval.id, opt)
    }
  }
  const choose = (optionId: string) => dispatchOption(optionId)
  const armOption = (optionId: string) => setArmedOptionId(optionId)
  const confirmOption = (optionId: string) => {
    setArmedOptionId(null)
    dispatchOption(optionId)
  }
  const cancelArm = () => setArmedOptionId(null)

  const decisionActions: ApprovalDecisionActions = {
    allowOnce,
    allowAllArm,
    allowAllConfirm,
    allowAllCancel,
    deny,
    armed,
    canResolveOptions: !!actions.resolveApprovalOption,
    choose,
    armedOptionId,
    armOption,
    confirmOption,
    cancelArm,
  }

  return (
    <div data-jcode-ui="" className={className} data-approval-id={approval.id}>
      {renderPending?.(approval, decisionActions) ?? DefaultPending({ approval, actions: decisionActions })}
    </div>
  )
}

function DefaultResolved({ approval }: { approval: Approval }): ReactNode {
  const optionLabel = approval.resolvedOptionId
    ? approval.options?.find((o) => o.id === approval.resolvedOptionId)?.label
    : undefined
  return (
    <span>
      {optionLabel ?? (approval.approved ? '✓ allowed' : '✗ denied')} · {approval.tool_name}
    </span>
  )
}

function DefaultPending({
  approval,
  actions,
}: {
  approval: Approval
  actions: ApprovalDecisionActions
}): ReactNode {
  const disabled = !!approval.resolving
  return (
    <div style={{ border: '1px solid', padding: 8 }}>
      <div>Approve {approval.tool_name}?</div>
      {approval.is_external && <div>⚠ external path</div>}
      <div style={{ display: 'flex', gap: 8, marginTop: 8, flexWrap: 'wrap' }}>
        {approval.options?.length ? (
          approval.options.map((o) =>
            (o.kind ?? 'custom') === 'allow_always' && actions.armedOptionId !== o.id ? (
              <button key={o.id} type="button" disabled={disabled} onClick={() => actions.armOption(o.id)}>
                {o.label}…
              </button>
            ) : (o.kind ?? 'custom') === 'allow_always' ? (
              <button key={o.id} type="button" disabled={disabled} style={{ color: 'red' }} onClick={() => actions.confirmOption(o.id)}>
                Confirm: {o.label}
              </button>
            ) : (
              <button key={o.id} type="button" disabled={disabled} onClick={() => actions.choose(o.id)}>
                {o.label}
              </button>
            ),
          )
        ) : (
          <>
            <button type="button" onClick={actions.allowOnce} disabled={disabled}>Allow once</button>
            {!actions.armed ? (
              <button type="button" onClick={actions.allowAllArm} disabled={disabled}>Allow all…</button>
            ) : (
              <>
                <button type="button" onClick={actions.allowAllConfirm} disabled={disabled} style={{ color: 'red' }}>
                  Confirm allow all
                </button>
                <button type="button" onClick={actions.allowAllCancel} disabled={disabled}>Cancel</button>
              </>
            )}
            <button type="button" onClick={actions.deny} disabled={disabled}>Deny</button>
          </>
        )}
      </div>
    </div>
  )
}
