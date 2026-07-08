/**
 * ApprovalBlock — the headless approval gate.
 *
 * Owns: the pending/resolved state split, the 3-tier decision (allow once / allow
 * all / deny), the "arming" UX for "allow all" (two-step confirm to prevent
 * accidental blanket approval), and dispatching via runtime actions. Does NOT
 * own styling or the tool-name→icon mapping (those live in the styled wrapper).
 *
 * The `resolving` flag on the approval object disables controls while a resolve
 * request is in flight (prevents double-submit).
 */

import { useState } from 'react'
import type { ReactNode } from 'react'
import type { Approval } from '../types/index.js'
import { useRuntimeActions } from '../runtime/context.js'

export interface ApprovalBlockRenderSlots {
  /** Render the pending decision card. Receives the action callbacks. */
  renderPending?: (
    approval: Approval,
    actions: {
      allowOnce: () => void
      allowAllArm: () => void
      allowAllConfirm: () => void
      allowAllCancel: () => void
      deny: () => void
      armed: boolean
    },
  ) => ReactNode
  /** Render the resolved inline note. */
  renderResolved?: (approval: Approval) => ReactNode
}

export interface ApprovalBlockProps extends ApprovalBlockRenderSlots {
  approval: Approval
  /** className passthrough. */
  className?: string
}

export function ApprovalBlock({ approval, className, renderPending, renderResolved }: ApprovalBlockProps): ReactNode {
  const actions = useRuntimeActions()
  // Arming state for "allow all" — the user must click twice (first arms,
  // turning the button destructive; second confirms).
  const [armed, setArmed] = useState(false)

  if (approval.resolved) {
    return <div className={className}>{renderResolved?.(approval) ?? <DefaultResolved approval={approval} />}</div>
  }

  const allowOnce = () => actions.resolveApproval(approval.id, true, false)
  const allowAllArm = () => setArmed(true)
  const allowAllConfirm = () => actions.resolveApproval(approval.id, true, true)
  const allowAllCancel = () => setArmed(false)
  const deny = () => actions.resolveApproval(approval.id, false, false)

  return (
    <div className={className} data-approval-id={approval.id}>
      {renderPending?.(approval, { allowOnce, allowAllArm, allowAllConfirm, allowAllCancel, deny, armed }) ??
        DefaultPending({ approval, allowOnce, allowAllArm, allowAllConfirm, allowAllCancel, deny, armed })}
    </div>
  )
}

function DefaultResolved({ approval }: { approval: Approval }): ReactNode {
  return (
    <span>
      {approval.approved ? '✓ allowed' : '✗ denied'} · {approval.tool_name}
    </span>
  )
}

function DefaultPending(args: {
  approval: Approval
  allowOnce: () => void
  allowAllArm: () => void
  allowAllConfirm: () => void
  allowAllCancel: () => void
  deny: () => void
  armed: boolean
}): ReactNode {
  const { approval, allowOnce, allowAllArm, allowAllConfirm, allowAllCancel, deny, armed } = args
  const disabled = !!approval.resolving
  return (
    <div style={{ border: '1px solid', padding: 8 }}>
      <div>Approve {approval.tool_name}?</div>
      {approval.is_external && <div>⚠ external path</div>}
      <div style={{ display: 'flex', gap: 8, marginTop: 8 }}>
        <button type="button" onClick={allowOnce} disabled={disabled}>Allow once</button>
        {!armed ? (
          <button type="button" onClick={allowAllArm} disabled={disabled}>Allow all…</button>
        ) : (
          <>
            <button type="button" onClick={allowAllConfirm} disabled={disabled} style={{ color: 'red' }}>
              Confirm allow all
            </button>
            <button type="button" onClick={allowAllCancel} disabled={disabled}>Cancel</button>
          </>
        )}
        <button type="button" onClick={deny} disabled={disabled}>Deny</button>
      </div>
    </div>
  )
}
