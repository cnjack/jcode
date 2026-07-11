/**
 * SpeechInput — push-to-dictate microphone button.
 *
 * Primary path is the Web Speech API (`SpeechRecognition` /
 * `webkitSpeechRecognition`) for live, continuous, on-device transcription with
 * interim results. When the browser lacks Speech Recognition (Firefox, most
 * desktop Safari builds) it gracefully degrades to `MediaRecorder`: it captures
 * a mic clip and hands the resulting `Blob` to `props.onAudio` so the host can
 * transcribe it server-side.
 *
 * State machine: idle → listening | recording → (idle | error). While active a
 * pulsing destructive dot renders and an `aria-live` region announces status.
 * SSR-safe: every browser API is probed behind a `typeof window` guard and only
 * touched inside event handlers / effects.
 */

import { memo, useCallback, useEffect, useRef, useState } from 'react'
import { MicrophoneIcon, StopIcon, ExclamationTriangleIcon } from '@heroicons/react/24/outline'

/* ─── Minimal Web Speech typings ───
 * The standard TS DOM lib does not ship SpeechRecognition, so we declare the
 * narrow surface we touch instead of pulling an extra @types dependency. */
interface SpeechAlternative {
  readonly transcript: string
  readonly confidence: number
}
interface SpeechResult {
  readonly isFinal: boolean
  readonly length: number
  readonly [index: number]: SpeechAlternative
}
interface SpeechResultList {
  readonly length: number
  readonly [index: number]: SpeechResult
}
interface SpeechRecognitionEventLike {
  readonly resultIndex: number
  readonly results: SpeechResultList
}
interface SpeechRecognitionErrorEventLike {
  readonly error: string
  readonly message: string
}
interface SpeechRecognitionLike {
  lang: string
  continuous: boolean
  interimResults: boolean
  maxAlternatives: number
  start(): void
  stop(): void
  abort(): void
  onresult: ((event: SpeechRecognitionEventLike) => void) | null
  onerror: ((event: SpeechRecognitionErrorEventLike) => void) | null
  onend: (() => void) | null
  onstart: (() => void) | null
}
type SpeechRecognitionCtor = new () => SpeechRecognitionLike

function getSpeechRecognitionCtor(): SpeechRecognitionCtor | null {
  if (typeof window === 'undefined') return null
  const w = window as unknown as {
    SpeechRecognition?: SpeechRecognitionCtor
    webkitSpeechRecognition?: SpeechRecognitionCtor
  }
  return w.SpeechRecognition ?? w.webkitSpeechRecognition ?? null
}

function canRecordAudio(): boolean {
  return (
    typeof window !== 'undefined' &&
    typeof navigator !== 'undefined' &&
    !!navigator.mediaDevices?.getUserMedia &&
    typeof window.MediaRecorder !== 'undefined'
  )
}

export type SpeechInputStatus = 'idle' | 'listening' | 'recording' | 'error'

export interface SpeechInputProps {
  /** BCP-47 language tag for recognition, e.g. `en-US`, `zh-CN`. Default `en-US`. */
  lang?: string
  /** Called with recognized text. `final` distinguishes stable vs interim results. */
  onTranscript: (text: string, meta: { final: boolean }) => void
  /** Fallback sink: when Speech Recognition is unavailable, the recorded clip is handed here. */
  onAudio?: (blob: Blob) => void
  /** Disable the control. */
  disabled?: boolean
  /** Optional label rendered next to the button. */
  label?: string
  className?: string
}

export const SpeechInput = memo(function SpeechInput({
  lang = 'en-US',
  onTranscript,
  onAudio,
  disabled = false,
  label,
  className,
}: SpeechInputProps) {
  const [status, setStatus] = useState<SpeechInputStatus>('idle')
  const [error, setError] = useState<string | null>(null)

  // Latest-prop refs keep the recognition/recorder callbacks stable.
  const onTranscriptRef = useRef(onTranscript)
  const onAudioRef = useRef(onAudio)
  const langRef = useRef(lang)
  onTranscriptRef.current = onTranscript
  onAudioRef.current = onAudio
  langRef.current = lang

  const recognitionRef = useRef<SpeechRecognitionLike | null>(null)
  const recorderRef = useRef<MediaRecorder | null>(null)
  const streamRef = useRef<MediaStream | null>(null)
  const chunksRef = useRef<Blob[]>([])
  const manualStopRef = useRef(false)

  const stopTracks = useCallback(() => {
    streamRef.current?.getTracks().forEach((t) => t.stop())
    streamRef.current = null
  }, [])

  const startListening = useCallback((Ctor: SpeechRecognitionCtor) => {
    setError(null)
    manualStopRef.current = false
    const recognition = new Ctor()
    recognition.lang = langRef.current
    recognition.continuous = true
    recognition.interimResults = true
    recognition.maxAlternatives = 1
    recognition.onresult = (event) => {
      let interim = ''
      let final = ''
      for (let i = event.resultIndex; i < event.results.length; i++) {
        const result = event.results[i]
        const chunk = result?.[0]?.transcript ?? ''
        if (result?.isFinal) final += chunk
        else interim += chunk
      }
      if (final) onTranscriptRef.current(final, { final: true })
      if (interim) onTranscriptRef.current(interim, { final: false })
    }
    recognition.onerror = (event) => {
      // `no-speech`/`aborted` are benign auto-stops; surface everything else.
      if (event.error === 'aborted' || event.error === 'no-speech') return
      setError(event.message || `Recognition error: ${event.error}`)
      setStatus('error')
    }
    recognition.onend = () => {
      recognitionRef.current = null
      setStatus((s) => (s === 'error' ? s : 'idle'))
    }
    recognitionRef.current = recognition
    try {
      recognition.start()
      setStatus('listening')
    } catch {
      setError('Could not start speech recognition.')
      setStatus('error')
    }
  }, [])

  const startRecording = useCallback(async () => {
    setError(null)
    manualStopRef.current = false
    try {
      const stream = await navigator.mediaDevices.getUserMedia({ audio: true })
      streamRef.current = stream
      const recorder = new MediaRecorder(stream)
      chunksRef.current = []
      recorder.ondataavailable = (e) => {
        if (e.data && e.data.size > 0) chunksRef.current.push(e.data)
      }
      recorder.onstop = () => {
        const type = recorder.mimeType || 'audio/webm'
        const blob = new Blob(chunksRef.current, { type })
        chunksRef.current = []
        if (blob.size > 0) onAudioRef.current?.(blob)
        stopTracks()
        recorderRef.current = null
        setStatus((s) => (s === 'error' ? s : 'idle'))
      }
      recorder.start()
      recorderRef.current = recorder
      setStatus('recording')
    } catch {
      stopTracks()
      setError('Microphone access was denied or is unavailable.')
      setStatus('error')
    }
  }, [stopTracks])

  const stop = useCallback(() => {
    manualStopRef.current = true
    recognitionRef.current?.stop()
    if (recorderRef.current && recorderRef.current.state !== 'inactive') {
      recorderRef.current.stop()
    }
  }, [])

  const toggle = useCallback(() => {
    if (disabled) return
    if (status === 'listening' || status === 'recording') {
      stop()
      return
    }
    const Ctor = getSpeechRecognitionCtor()
    if (Ctor) {
      startListening(Ctor)
    } else if (onAudioRef.current && canRecordAudio()) {
      void startRecording()
    } else {
      setError('Speech recognition is not supported in this browser.')
      setStatus('error')
    }
  }, [disabled, status, stop, startListening, startRecording])

  // Cleanup on unmount.
  useEffect(() => {
    return () => {
      manualStopRef.current = true
      try {
        recognitionRef.current?.abort()
      } catch {
        /* no-op */
      }
      if (recorderRef.current && recorderRef.current.state !== 'inactive') {
        try {
          recorderRef.current.stop()
        } catch {
          /* no-op */
        }
      }
      streamRef.current?.getTracks().forEach((t) => t.stop())
    }
  }, [])

  const active = status === 'listening' || status === 'recording'
  const liveMessage =
    status === 'listening'
      ? 'Listening…'
      : status === 'recording'
        ? 'Recording…'
        : status === 'error'
          ? error ?? 'Error'
          : 'Idle'

  const buttonLabel = active
    ? 'Stop dictation'
    : status === 'error'
      ? 'Retry dictation'
      : 'Start dictation'

  return (
    <div
      data-jcode-ui=""
      className={['jcode-voice-speech', className].filter(Boolean).join(' ')}
      data-status={status}
    >
      <button
        type="button"
        className="jcode-voice-speech__btn"
        onClick={toggle}
        disabled={disabled}
        aria-label={buttonLabel}
        aria-pressed={active}
        title={buttonLabel}
      >
        {active && <span className="jcode-voice-speech__dot" aria-hidden="true" />}
        {status === 'error' ? (
          <ExclamationTriangleIcon className="jcode-voice-speech__icon" aria-hidden="true" />
        ) : active ? (
          <StopIcon className="jcode-voice-speech__icon" aria-hidden="true" />
        ) : (
          <MicrophoneIcon className="jcode-voice-speech__icon" aria-hidden="true" />
        )}
      </button>
      {label && <span className="jcode-voice-speech__label">{label}</span>}
      {status === 'error' && error && (
        <span className="jcode-voice-speech__error" role="alert">
          {error}
        </span>
      )}
      <span className="jcode-voice-sr" role="status" aria-live="polite">
        {liveMessage}
      </span>
    </div>
  )
})
