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
import type { Approval, ApprovalOption } from 'jcode-ui-core'
import { ApprovalBlock } from 'jcode-ui-core/primitives'
import type { ApprovalDecisionActions } from 'jcode-ui-core/primitives'

export interface ApprovalBannerProps {
  approval: Approval
}

export const ApprovalBanner = memo(function ApprovalBanner({ approval }: ApprovalBannerProps) {
  return (
    <ApprovalBlock
      approval={approval}
      className="jcode-approval"
      renderPending={(a, acts) => <PendingCard approval={a} actions={acts} />}
      renderResolved={(a) => <ResolvedNote approval={a} />}
    />
  )
})

/** Button class per option kind — allow reads as the primary action, deny as
 *  destructive-outline, everything else stays neutral. */
function optionButtonClass(kind: ApprovalOption['kind']): string {
  switch (kind) {
    case 'allow_once':
      return 'jcode-btn jcode-btn-allow'
    case 'deny':
      return 'jcode-btn jcode-btn-deny'
    default:
      return 'jcode-btn jcode-btn-ghost'
  }
}

function PendingCard({ approval, actions }: { approval: Approval; actions: ApprovalDecisionActions }) {
  const target = useMemo(() => extractTarget(approval), [approval])
  const Icon = toolIcon(approval.tool_name)
  const disabled = !!approval.resolving
  const armed = actions.armed || actions.armedOptionId !== null
  const hasOptions = !!approval.options?.length
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
        {hasOptions ? (
          approval.options!.map((o) => {
            const kind = o.kind ?? 'custom'
            if (kind === 'allow_always') {
              // Blanket approvals keep the two-step arming UX regardless of host.
              return actions.armedOptionId === o.id ? (
                <span key={o.id} className="contents">
                  <button
                    type="button"
                    onClick={() => actions.confirmOption(o.id)}
                    disabled={disabled}
                    className="jcode-btn jcode-btn-caution"
                  >
                    Confirm: {o.label}
                  </button>
                  <button
                    type="button"
                    onClick={actions.cancelArm}
                    disabled={disabled}
                    className="jcode-btn jcode-btn-ghost"
                  >
                    Cancel
                  </button>
                </span>
              ) : (
                <button
                  key={o.id}
                  type="button"
                  onClick={() => actions.armOption(o.id)}
                  disabled={disabled}
                  title={o.description}
                  className="jcode-btn jcode-btn-ghost"
                >
                  {o.label}…
                </button>
              )
            }
            return (
              <button
                key={o.id}
                type="button"
                onClick={() => actions.choose(o.id)}
                disabled={disabled}
                title={o.description}
                className={optionButtonClass(kind)}
              >
                {o.label}
              </button>
            )
          })
        ) : (
          <>
            <button type="button" onClick={actions.allowOnce} disabled={disabled} className="jcode-btn jcode-btn-allow">
              Allow once
            </button>
            {!actions.armed ? (
              <button type="button" onClick={actions.allowAllArm} disabled={disabled} className="jcode-btn jcode-btn-ghost">
                Allow all…
              </button>
            ) : (
              <>
                <button
                  type="button"
                  onClick={actions.allowAllConfirm}
                  disabled={disabled}
                  className="jcode-btn jcode-btn-caution"
                >
                  Confirm allow all
                </button>
                <button type="button" onClick={actions.allowAllCancel} disabled={disabled} className="jcode-btn jcode-btn-ghost">
                  Cancel
                </button>
              </>
            )}
            <button type="button" onClick={actions.deny} disabled={disabled} className="jcode-btn jcode-btn-deny">
              Deny
            </button>
          </>
        )}
      </div>
    </div>
  )
}

function ResolvedNote({ approval }: { approval: Approval }) {
  const chosen = approval.resolvedOptionId
    ? approval.options?.find((o) => o.id === approval.resolvedOptionId)
    : undefined
  const ok = chosen ? (chosen.kind ?? 'custom') !== 'deny' : approval.approved
  const Icon = ok ? ShieldCheckIcon : ShieldExclamationIcon
  return (
    <div className={`jcode-approval-resolved${ok ? ' is-ok' : ''}`}>
      <Icon className="h-3.5 w-3.5 shrink-0" />
      <span>
        {chosen?.label ?? (ok ? 'Allowed' : 'Denied')}
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
