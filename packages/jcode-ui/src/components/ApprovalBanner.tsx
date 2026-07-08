/**
 * ApprovalBanner — styled approval gate (wraps headless ApprovalBlock).
 *
 * Pending: warning-tinted decision card with a tool-identity tile (icon keyed
 * off tool_name), the primary target pulled from args, an external-path chip,
 * and the 3-tier button ramp (allow once / allow all [armed] / deny). Resolved:
 * collapses to a borderless inline note.
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
      className="jcode-approval px-4 py-1"
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
    <div className="my-1 rounded-[var(--radius-lg)] border border-[var(--color-warning-fg)] bg-[var(--color-warning-bg)] px-3.5 py-2.5">
      <div className="flex items-center gap-2">
        <Icon className="h-4 w-4 shrink-0 text-[var(--color-warning-fg)]" />
        <span className="text-[0.82rem] font-medium text-[var(--color-foreground)]">
          Approve <span className="text-[var(--color-warning-fg)]">{verbOf(approval.tool_name)}</span>
        </span>
        {target && (
          <code className="ml-1 max-w-[50%] truncate rounded-[var(--radius-xs)] bg-[var(--color-muted)] px-1.5 py-0.5 font-mono text-[0.72rem] text-[var(--color-foreground)]">
            {target}
          </code>
        )}
        {approval.is_external && (
          <span className="ml-auto shrink-0 rounded-[var(--radius-xs)] bg-[var(--color-error-bg)] px-1.5 py-0.5 text-[0.66rem] text-[var(--color-error-fg)]">
            external
          </span>
        )}
      </div>
      <details className="mt-1.5">
        <summary className="cursor-pointer text-[0.7rem] text-[var(--color-muted-foreground)]">full arguments</summary>
        <pre className="mt-1 overflow-auto rounded-[var(--radius-md)] bg-[var(--code-bg)] px-2 py-1 font-mono text-[0.7rem] text-[var(--color-foreground)]">{prettyArgs(approval.tool_args)}</pre>
      </details>
      <div className="mt-2.5 flex flex-wrap gap-2">
        <button
          type="button"
          onClick={allowOnce}
          disabled={disabled}
          className="rounded-[var(--radius-md)] bg-[var(--color-primary)] px-3 py-1 text-[0.8rem] font-medium text-[var(--color-on-primary)] hover:bg-[var(--accent-wash-strong)] disabled:opacity-50"
        >
          Allow once
        </button>
        {!armed ? (
          <button
            type="button"
            onClick={allowAllArm}
            disabled={disabled}
            className="rounded-[var(--radius-md)] border border-[var(--color-border)] px-3 py-1 text-[0.8rem] text-[var(--color-foreground)] hover:bg-[var(--neutral-wash-soft)] disabled:opacity-50"
          >
            Allow all…
          </button>
        ) : (
          <>
            <button
              type="button"
              onClick={allowAllConfirm}
              disabled={disabled}
              className="rounded-[var(--radius-md)] bg-[var(--color-destructive)] px-3 py-1 text-[0.8rem] font-medium text-[var(--color-on-destructive)] hover:opacity-90 disabled:opacity-50"
            >
              Confirm allow all
            </button>
            <button
              type="button"
              onClick={allowAllCancel}
              disabled={disabled}
              className="rounded-[var(--radius-md)] px-3 py-1 text-[0.8rem] text-[var(--color-muted-foreground)] hover:bg-[var(--neutral-wash-soft)] disabled:opacity-50"
            >
              Cancel
            </button>
          </>
        )}
        <button
          type="button"
          onClick={deny}
          disabled={disabled}
          className="ml-auto rounded-[var(--radius-md)] px-3 py-1 text-[0.8rem] text-[var(--color-error-fg)] hover:bg-[var(--color-error-bg)] disabled:opacity-50"
        >
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
    <div className="flex items-center gap-1.5 px-4 py-0.5 text-[0.78rem]">
      <Icon className={`h-3.5 w-3.5 ${ok ? 'text-[var(--color-success)]' : 'text-[var(--color-muted-foreground)]'}`} />
      <span className="text-[var(--color-muted-foreground)]">
        {ok ? 'allowed' : 'denied'} · {approval.tool_name}
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
      return 'command'
    case 'write':
      return 'write'
    case 'edit':
    case 'multi_edit':
      return 'edit'
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
