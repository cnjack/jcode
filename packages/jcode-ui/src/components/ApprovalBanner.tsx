/**
 * ApprovalBanner — human-in-the-loop approval gate.
 *
 * Soft surface card (matches chat surface), neutral chrome, accent only on the
 * primary action — avoids the "black card + loud orange" clash.
 */

import { memo, useMemo } from 'react'
import {
  ShieldCheckIcon,
  ShieldExclamationIcon,
  CommandLineIcon,
  DocumentTextIcon,
  PencilSquareIcon,
} from '@heroicons/react/24/outline'
import type { Approval } from 'jcode-ui-core'
import { ApprovalBlock } from 'jcode-ui-core/primitives'

export interface ApprovalBannerProps {
  approval: Approval
}

export const ApprovalBanner = memo(function ApprovalBanner({ approval }: ApprovalBannerProps) {
  return (
    <ApprovalBlock
      approval={approval}
      className="jcode-approval"
      renderPending={(a, acts) => <PendingCard approval={a} {...acts} />}
      renderResolved={(a) => <ResolvedNote approval={a} />}
    />
  )
})

function PendingCard({
  approval,
  allowOnce,
  allowAllArm,
  allowAllConfirm,
  allowAllCancel,
  deny,
  armed,
}: {
  approval: Approval
  allowOnce: () => void
  allowAllArm: () => void
  allowAllConfirm: () => void
  allowAllCancel: () => void
  deny: () => void
  armed: boolean
}) {
  const target = useMemo(() => extractTarget(approval), [approval])
  const Icon = toolIcon(approval.tool_name)
  const disabled = !!approval.resolving
  return (
    <div className={`jcode-approval-card${armed ? ' is-armed' : ''}${approval.is_external ? ' is-external' : ''}`}>
      <div className="jcode-approval-card__head">
        <span className="jcode-approval-card__icon" aria-hidden>
          <Icon className="h-4 w-4" />
        </span>
        <div className="jcode-approval-card__titles">
          <span className="jcode-approval-card__label">Approval needed</span>
          <span className="jcode-approval-card__verb">
            {verbOf(approval.tool_name)}
            {target ? (
              <code className="jcode-approval-card__target" title={target}>
                {target}
              </code>
            ) : null}
          </span>
        </div>
        {approval.is_external && <span className="jcode-approval-card__badge">external</span>}
      </div>

      <details className="jcode-approval-card__details">
        <summary>Arguments</summary>
        <pre>{prettyArgs(approval.tool_args)}</pre>
      </details>

      <div className="jcode-approval-card__actions">
        <button type="button" onClick={allowOnce} disabled={disabled} className="jcode-btn jcode-btn-allow">
          Allow once
        </button>
        {!armed ? (
          <button type="button" onClick={allowAllArm} disabled={disabled} className="jcode-btn jcode-btn-ghost">
            Allow all…
          </button>
        ) : (
          <>
            <button
              type="button"
              onClick={allowAllConfirm}
              disabled={disabled}
              className="jcode-btn jcode-btn-caution"
            >
              Confirm allow all
            </button>
            <button type="button" onClick={allowAllCancel} disabled={disabled} className="jcode-btn jcode-btn-ghost">
              Cancel
            </button>
          </>
        )}
        <button type="button" onClick={deny} disabled={disabled} className="jcode-btn jcode-btn-deny">
          Deny
        </button>
      </div>
    </div>
  )
}

function ResolvedNote({ approval }: { approval: Approval }) {
  const ok = approval.approved
  const Icon = ok ? ShieldCheckIcon : ShieldExclamationIcon
  return (
    <div className={`jcode-approval-resolved${ok ? ' is-ok' : ''}`}>
      <Icon className="h-3.5 w-3.5 shrink-0" />
      <span>
        {ok ? 'Allowed' : 'Denied'}
        <span className="jcode-approval-resolved__sep">·</span>
        <span className="jcode-approval-resolved__name">{approval.tool_name}</span>
      </span>
    </div>
  )
}

function extractTarget(approval: Approval): string {
  try {
    const a = JSON.parse(approval.tool_args)
    return a.command ?? a.path ?? a.file_path ?? a.pattern ?? ''
  } catch {
    return ''
  }
}

function verbOf(name: string): string {
  switch (name) {
    case 'execute':
      return 'Run command'
    case 'write':
      return 'Write file'
    case 'edit':
    case 'multi_edit':
      return 'Edit file'
    default:
      return name
  }
}

function toolIcon(name: string) {
  switch (name) {
    case 'execute':
      return CommandLineIcon
    case 'write':
      return DocumentTextIcon
    case 'edit':
    case 'multi_edit':
      return PencilSquareIcon
    default:
      return ShieldExclamationIcon
  }
}

function prettyArgs(args: string): string {
  try {
    return JSON.stringify(JSON.parse(args), null, 2)
  } catch {
    return args
  }
}
