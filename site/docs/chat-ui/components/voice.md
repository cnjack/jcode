---
title: Voice
parent: Components
nav_order: 21
---

# Voice

An optional voice suite (`jcode-ui/voice`): browser-native, **zero-dependency** components for dictation, transcript display, playback, and live mic visualization.

> No live preview here — these components touch `getUserMedia` / the Web Speech API, which need a real mic and user gesture. The examples below run in your app.

## Install

Nothing to install beyond `jcode-ui` — the voice suite is browser-native (Web Speech API + `MediaRecorder` + Web Audio, all probed behind `typeof window` guards, SSR-safe). It ships as an **opt-in subentry** so it's excluded from the main bundle unless imported.

## Import

```tsx
import 'jcode-ui/voice.css'
import {
  SpeechInput,
  Transcription,
  AudioPlayer,
  VoiceVisualizer,
} from 'jcode-ui/voice'
```

Every root carries `data-jcode-ui`, so the `--jcode-*` tokens resolve and dark mode applies automatically — no wrapper required.

## Minimal example

```tsx
import 'jcode-ui/voice.css'
import { SpeechInput } from 'jcode-ui/voice'

// Push-to-dictate into the composer. Falls back to MediaRecorder + onAudio
// when the browser lacks Speech Recognition (Firefox, most desktop Safari).
<SpeechInput
  lang="en-US"
  onTranscript={(text, { final }) => {
    if (final) appendToComposer(text)
  }}
  onAudio={(blob) => uploadForServerTranscription(blob)}
/>
```

## Components

| Export | Purpose |
|--------|---------|
| `SpeechInput` | Push-to-dictate mic button (Web Speech API, with `MediaRecorder` fallback). |
| `Transcription` | Segmented transcript list with an active-segment highlight and click-to-seek. |
| `AudioPlayer` | Minimal token-styled audio player emitting playback ticks. |
| `VoiceVisualizer` | Live mic waveform bars (Web Audio), with an idle breathing state. |

### `SpeechInput`

| Prop | Type | Default | Notes |
|------|------|---------|-------|
| `onTranscript` | `(text: string, meta: { final: boolean }) => void` | — | Recognized text; `final` distinguishes stable vs interim. |
| `onAudio` | `(blob: Blob) => void` | — | Fallback sink when Speech Recognition is unavailable. |
| `lang` | `string` | `'en-US'` | BCP-47 language tag (e.g. `zh-CN`). |
| `disabled` | `boolean` | `false` | Disable the control. |
| `label` | `string` | — | Optional label next to the button. |
| `className` | `string` | — | Extra class on the root. |

State machine: `idle → listening | recording → (idle | error)`, announced via an `aria-live` region.

### `Transcription`

| Prop | Type | Notes |
|------|------|-------|
| `segments` | `TranscriptSegment[]` | The transcript. |
| `currentTimeMs` | `number` | Playback position — drives the active highlight. |
| `onSeek` | `(ms: number) => void` | Seek handler; a segment is clickable when both this and `startMs` exist. |
| `className` | `string` | Extra class on the root. |

```ts
interface TranscriptSegment {
  id: string
  text: string
  startMs?: number
  endMs?: number
  speaker?: string   // diarization tag
}
```

### `AudioPlayer`

| Prop | Type | Default | Notes |
|------|------|---------|-------|
| `src` | `string` | — | Audio source URL (or object URL / data URI). |
| `onTimeUpdate` | `(ms: number) => void` | — | Fires on every tick and seek (ms). |
| `autoPlay` | `boolean` | `false` | Autoplay once metadata is ready (subject to browser policy). |
| `className` | `string` | — | Extra class on the root. |

Pair `AudioPlayer`'s `onTimeUpdate` with `Transcription`'s `currentTimeMs` / `onSeek` for a synced player + transcript.

### `VoiceVisualizer`

| Prop | Type | Default | Notes |
|------|------|---------|-------|
| `stream` | `MediaStream \| null` | — | Live input to analyze (e.g. from `getUserMedia`). Omit for the idle state. |
| `active` | `boolean` | `true` | `false` renders the idle breathing state even with a stream present. |
| `bars` | `number` | `32` | Number of bars to render. |
| `className` | `string` | — | Extra class on the root. |

## Related

- [ChatInput](/chat-ui/docs/components/chat-input) — wire `SpeechInput` into the composer
