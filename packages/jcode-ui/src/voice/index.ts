/**
 * jcode-ui/voice — optional voice suite (opt-in subentry).
 *
 * Browser-native, zero-dependency components for dictation, transcript display,
 * playback, and live mic visualization. Styles ship separately:
 *
 *   import { SpeechInput, Transcription, AudioPlayer, VoiceVisualizer } from 'jcode-ui/voice'
 *   import 'jcode-ui/voice.css'
 *
 * Every root carries `data-jcode-ui`, so the `--jcode-*` design tokens resolve
 * and dark mode applies automatically — no wrapper required.
 */

export { SpeechInput } from './SpeechInput.js'
export type { SpeechInputProps, SpeechInputStatus } from './SpeechInput.js'

export { Transcription } from './Transcription.js'
export type { TranscriptionProps, TranscriptSegment } from './Transcription.js'

export { AudioPlayer } from './AudioPlayer.js'
export type { AudioPlayerProps } from './AudioPlayer.js'

export { VoiceVisualizer } from './VoiceVisualizer.js'
export type { VoiceVisualizerProps } from './VoiceVisualizer.js'
