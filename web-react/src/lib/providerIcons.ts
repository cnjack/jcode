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

export function iconForProvider(id: string, fallbackOpenai = false): string | null {
  const key = (id || '').toLowerCase()
  for (const [re, svg] of RULES) {
    if (re.test(key)) return svg
  }
  return fallbackOpenai ? openai : null
}
