import { useCallback, useEffect, useMemo, useState } from 'react'
import { GeneratedImageCard } from 'jcode-ui'
import type { GeneratedImageState } from 'jcode-ui'
import type { ToolRendererProps } from 'jcode-ui-core/adapters'
import { useTranslation } from 'react-i18next'
import { useAppDispatch, useAppSelector } from '../app/hooks'
import { uiActions } from '../app/store'
import { api } from '../lib/api'
import { isTauri } from '../lib/useDesktop'

export function GeneratedImageToolRenderer({
  args,
  phase,
  outcome,
  errorCode,
  provider,
  model,
  artifacts,
  startedAt,
}: ToolRendererProps) {
  const { t } = useTranslation()
  const dispatch = useAppDispatch()
  const taskID = useAppSelector((state) => state.session.currentSessionId)
  const artifact = artifacts?.[0]
  const [imageURL, setImageURL] = useState('')
  const [assetError, setAssetError] = useState('')
  const input = useMemo(() => parseImageArgs(args), [args])
  const state = imageState(phase, outcome, !!artifact)

  useEffect(() => {
    let active = true
    let objectURL = ''
    setImageURL('')
    setAssetError('')
    if (state !== 'succeeded' || !artifact?.id || !taskID) return () => { active = false }
    void api.artifactContent(taskID, artifact.id).then((blob) => {
      if (!active) return
      objectURL = URL.createObjectURL(blob)
      setImageURL(objectURL)
    }).catch(() => {
      if (active) setAssetError(t('chat.generatedImage.assetError'))
    })
    return () => {
      active = false
      if (objectURL) URL.revokeObjectURL(objectURL)
    }
  }, [artifact?.id, state, t, taskID])

  const openImage = useCallback(() => {
    if (!artifact?.id || !taskID || !imageURL) return
    setAssetError('')
    if (!isTauri) {
      // Keep this synchronous in the user's click stack so browser popup
      // blockers allow the authenticated Blob URL to open.
      window.open(imageURL, '_blank', 'noopener,noreferrer')
      return
    }
    void api.openArtifact(taskID, artifact.id).catch(() => {
      setAssetError(t('chat.generatedImage.openError'))
    })
  }, [artifact?.id, imageURL, t, taskID])

  const openSettings = useCallback(() => {
    dispatch(uiActions.setSettingsTab('providers'))
    dispatch(uiActions.setView('settings'))
  }, [dispatch])

  if (shouldHideGeneratedImageCard({ phase, outcome, errorCode })) return null

  return (
    <GeneratedImageCard
      state={state}
      provider={artifact?.provider || provider}
      model={artifact?.model || model}
      aspectRatio={input.aspectRatio}
      startedAt={startedAt}
      imageSrc={imageURL || undefined}
      title={artifact?.title || t('chat.generatedImage.title')}
      alt={artifact?.title || t('chat.generatedImage.title')}
      artifact={artifact}
      errorCode={errorCode}
      errorMessage={state === 'failed' ? t('chat.generatedImage.failedHint') : undefined}
      assetError={assetError || undefined}
      strings={{
        queued: t('chat.generatedImage.queued'),
        queuedHint: t('chat.generatedImage.queuedHint'),
        generating: t('chat.generatedImage.generating'),
        saving: t('chat.generatedImage.saving'),
        savingHint: t('chat.generatedImage.savingHint'),
        succeeded: t('chat.generatedImage.succeeded'),
        failed: t('chat.generatedImage.failed'),
        uncertain: t('chat.generatedImage.uncertain'),
        uncertainHint: t('chat.generatedImage.uncertainHint'),
        cancelled: t('chat.generatedImage.cancelled'),
        cancelledHint: t('chat.generatedImage.cancelledHint'),
        authError: t('chat.generatedImage.authError'),
        quotaError: t('chat.generatedImage.quotaError'),
        safetyError: t('chat.generatedImage.safetyError'),
        rateLimitError: t('chat.generatedImage.rateLimitError'),
        downloadError: t('chat.generatedImage.providerDownloadError'),
        persistError: t('chat.generatedImage.persistError'),
        genericError: t('chat.generatedImage.failedHint'),
        loadingAsset: t('chat.generatedImage.loadingAsset'),
        assetError: t('chat.generatedImage.assetError'),
        download: t('chat.generatedImage.download'),
        openImage: t('chat.generatedImage.openImage'),
        openArtifact: t('chat.generatedImage.openArtifact'),
        reveal: t('chat.generatedImage.reveal'),
        openSettings: t('chat.generatedImage.openSettings'),
      }}
      onOpenImage={state === 'succeeded' && artifact && imageURL ? openImage : undefined}
      onOpenSettings={openSettings}
    />
  )
}

export function shouldHideGeneratedImageCard(
  lifecycle: Pick<ToolRendererProps, 'phase' | 'outcome' | 'errorCode'>,
): boolean {
  // In the image lifecycle, queued is always pre-dispatch. The independent
  // ApprovalBanner owns that state; the media placeholder starts at generating.
  if (lifecycle.phase === 'queued') return true
  return lifecycle.phase === 'terminal' &&
    lifecycle.outcome === 'cancelled' &&
    (lifecycle.errorCode === 'approval_denied' || lifecycle.errorCode === 'cancelled_before_dispatch')
}

function imageState(phase: ToolRendererProps['phase'], outcome: ToolRendererProps['outcome'], hasArtifact: boolean): GeneratedImageState {
  if (phase === 'queued' || phase === 'generating' || phase === 'saving') return phase
  if (outcome) return outcome
  return hasArtifact ? 'succeeded' : 'uncertain'
}

function parseImageArgs(raw: string): { aspectRatio?: string } {
  try {
    const value = JSON.parse(raw) as Record<string, unknown>
    const aspect = typeof value.aspect_ratio === 'string' ? value.aspect_ratio : undefined
    const size = typeof value.size === 'string' ? value.size.replace(/[×x]/, ':') : undefined
    return {
      aspectRatio: aspect || size,
    }
  } catch {
    return {}
  }
}
