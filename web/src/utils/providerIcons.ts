// Brand logos for the model providers, sourced from @lobehub/icons-static-svg
// (the same official-brand icon set used by LobeChat and friends, MIT-licensed).
//
// Each SVG is imported as a raw string (Vite `?raw`) so it can be inlined into
// the DOM. Inlining is what lets the monochrome marks inherit `currentColor` and
// follow the app's light/dark theme — an <img> tag cannot do that.
import openai from '@lobehub/icons-static-svg/icons/openai.svg?raw'
import anthropic from '@lobehub/icons-static-svg/icons/anthropic.svg?raw'
import gemini from '@lobehub/icons-static-svg/icons/gemini-color.svg?raw'
import deepseek from '@lobehub/icons-static-svg/icons/deepseek-color.svg?raw'
import zhipu from '@lobehub/icons-static-svg/icons/zhipu-color.svg?raw'
import zai from '@lobehub/icons-static-svg/icons/zai.svg?raw'
import mistral from '@lobehub/icons-static-svg/icons/mistral-color.svg?raw'
import openrouter from '@lobehub/icons-static-svg/icons/openrouter.svg?raw'
import groq from '@lobehub/icons-static-svg/icons/groq.svg?raw'
import together from '@lobehub/icons-static-svg/icons/together-color.svg?raw'
import qwen from '@lobehub/icons-static-svg/icons/qwen-color.svg?raw'
import moonshot from '@lobehub/icons-static-svg/icons/moonshot.svg?raw'
import minimax from '@lobehub/icons-static-svg/icons/minimax-color.svg?raw'
import siliconcloud from '@lobehub/icons-static-svg/icons/siliconcloud-color.svg?raw'
import hunyuan from '@lobehub/icons-static-svg/icons/hunyuan-color.svg?raw'
import xiaomi from '@lobehub/icons-static-svg/icons/xiaomimimo.svg?raw'
import ollama from '@lobehub/icons-static-svg/icons/ollama.svg?raw'

// Ordered keyword → icon rules. A provider id from the registry is matched by
// substring (first hit wins), so brand variants like `zhipuai-coding-plan`,
// `alibaba-coding-plan-cn`, `minimax-coding-plan`, or `tencent-tokenhub` all map
// to a single brand without needing an entry per variant. Order matters where
// one brand's keyword could appear inside another's id — keep `zhipu` ahead of
// the looser `zai`.
const RULES: ReadonlyArray<readonly [RegExp, string]> = [
  [/openrouter/, openrouter],
  [/openai/, openai],
  [/anthropic|claude/, anthropic],
  [/gemini|google|vertex/, gemini],
  [/deepseek/, deepseek],
  [/zhipu/, zhipu],
  [/zai/, zai],
  [/mistral/, mistral],
  [/groq/, groq],
  [/together/, together],
  [/alibaba|qwen|dashscope|tongyi/, qwen],
  [/moonshot|kimi/, moonshot],
  [/minimax/, minimax],
  [/silicon/, siliconcloud],
  [/tencent|hunyuan/, hunyuan],
  [/xiaomi|mimo/, xiaomi],
  [/ollama/, ollama],
]

// Returns the raw SVG markup for a provider id, or null when no brand icon is
// known (e.g. a custom OpenAI-compatible endpoint). Callers render a monogram
// fallback in that case.
export function iconForProvider(id: string): string | null {
  const key = (id || '').toLowerCase()
  for (const [re, svg] of RULES) {
    if (re.test(key)) return svg
  }
  return null
}
