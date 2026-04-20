<script setup lang="ts">
import { ref, nextTick, watch, computed, onMounted, onUnmounted } from 'vue'
import { useChatStore } from '@/stores/chat'
import { api } from '@/composables/api'
import type { SkillInfo } from '@/types/api'

const store = useChatStore()
const input = ref('')
const textarea = ref<HTMLTextAreaElement | null>(null)
const showModelPicker = ref(false)
const showModePicker = ref(false)
const containerRef = ref<HTMLDivElement | null>(null)

const skills = ref<SkillInfo[]>([])
const showSlashMenu = ref(false)
const slashFilter = ref('')
const selectedSlashIdx = ref(0)

const modes = [
  { value: 'agent' as const, label: 'Agent', icon: '⚡' },
  { value: 'plan' as const, label: 'Plan', icon: '📋' },
]

const filteredSlashCommands = computed(() => {
  const filter = slashFilter.value.toLowerCase()
  return skills.value.filter(
    (s) => s.slash && s.slash.toLowerCase().includes(filter),
  )
})

function autoResize() {
  const el = textarea.value
  if (!el) return
  el.style.height = 'auto'
  el.style.height = Math.min(el.scrollHeight, 160) + 'px'
}

function handleKeyDown(e: KeyboardEvent) {
  if (showSlashMenu.value) {
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      selectedSlashIdx.value = Math.min(selectedSlashIdx.value + 1, filteredSlashCommands.value.length - 1)
      return
    }
    if (e.key === 'ArrowUp') {
      e.preventDefault()
      selectedSlashIdx.value = Math.max(selectedSlashIdx.value - 1, 0)
      return
    }
    if (e.key === 'Enter' || e.key === 'Tab') {
      const cmd = filteredSlashCommands.value[selectedSlashIdx.value]
      if (cmd) {
        e.preventDefault()
        applySlashCommand(cmd)
        return
      }
    }
    if (e.key === 'Escape') {
      e.preventDefault()
      showSlashMenu.value = false
      return
    }
  }

  if (e.key === 'Enter' && !e.shiftKey) {
    e.preventDefault()
    send()
  }
}

function handleInput() {
  autoResize()
  const text = input.value
  if (text.startsWith('/')) {
    slashFilter.value = text.slice(1)
    showSlashMenu.value = true
    selectedSlashIdx.value = 0
  } else {
    showSlashMenu.value = false
  }
}

function applySlashCommand(skill: SkillInfo) {
  input.value = skill.slash + ' '
  showSlashMenu.value = false
  nextTick(() => textarea.value?.focus())
}

async function send() {
  const text = input.value.trim()
  if (!text || store.isRunning) return
  input.value = ''
  showSlashMenu.value = false
  await nextTick()
  autoResize()
  store.sendMessage(text)
}

function selectModel(provider: string, model: string) {
  showModelPicker.value = false
  store.switchModel(provider, model)
}

function selectMode(mode: 'agent' | 'plan') {
  showModePicker.value = false
  store.switchMode(mode)
}

function handleClickOutside(e: MouseEvent) {
  if (containerRef.value && !containerRef.value.contains(e.target as Node)) {
    showModelPicker.value = false
    showModePicker.value = false
    showSlashMenu.value = false
  }
}

function handleGlobalKey(e: KeyboardEvent) {
  if ((e.ctrlKey || e.metaKey) && e.key === 'l') {
    e.preventDefault()
    textarea.value?.focus()
  }
}

onMounted(async () => {
  document.addEventListener('click', handleClickOutside)
  document.addEventListener('keydown', handleGlobalKey)
  try {
    skills.value = await api.skillsList()
  } catch { /* ignore */ }
})

onUnmounted(() => {
  document.removeEventListener('click', handleClickOutside)
  document.removeEventListener('keydown', handleGlobalKey)
})

watch(() => store.isRunning, (running) => {
  if (!running) nextTick(() => textarea.value?.focus())
})
</script>

<template>
  <div ref="containerRef" class="border-t border-zinc-200 dark:border-zinc-800/80 bg-white/80 dark:bg-zinc-900/80 backdrop-blur-sm px-5 py-3">
    <div class="max-w-3xl mx-auto">
      <div class="bg-zinc-50 dark:bg-zinc-800/60 border border-zinc-200 dark:border-zinc-700/60 rounded-md px-3.5 py-2.5 transition-all relative">
        <!-- Slash command menu -->
        <div
          v-if="showSlashMenu && filteredSlashCommands.length > 0"
          class="absolute bottom-full mb-2 left-0 right-0 z-30 bg-white dark:bg-zinc-800 border border-zinc-200 dark:border-zinc-700 rounded-md shadow-lg dark:shadow-2xl py-1.5 max-h-48 overflow-y-auto"
        >
          <button
            v-for="(cmd, i) in filteredSlashCommands"
            :key="cmd.name"
            class="w-full px-3.5 py-2 text-left flex items-start gap-2.5 cursor-pointer transition-colors"
            :class="i === selectedSlashIdx
              ? 'bg-emerald-50 dark:bg-emerald-500/10'
              : 'hover:bg-zinc-50 dark:hover:bg-zinc-700/50'"
            @click="applySlashCommand(cmd)"
            @mouseenter="selectedSlashIdx = i"
          >
            <span class="text-xs font-mono text-emerald-600 dark:text-emerald-400 shrink-0">{{ cmd.slash }}</span>
            <span class="text-[11px] text-zinc-500 dark:text-zinc-400 truncate">{{ cmd.description }}</span>
          </button>
        </div>

        <textarea
          ref="textarea"
          v-model="input"
          :placeholder="store.isRunning ? 'Agent is working…' : 'Ask anything… (/ for commands)'"
          rows="1"
          :disabled="store.isRunning"
          class="w-full bg-transparent text-zinc-800 dark:text-zinc-100 text-sm resize-none outline-none placeholder-zinc-400 dark:placeholder-zinc-500 min-h-6 max-h-40 leading-relaxed disabled:opacity-50"
          @keydown="handleKeyDown"
          @input="handleInput"
        />
        <!-- Toolbar row -->
        <div class="flex items-center justify-between mt-1.5 pt-1.5 border-t border-zinc-200/60 dark:border-zinc-700/40">
          <div class="flex items-center gap-1.5">
            <!-- Mode selector -->
            <div class="relative">
              <button
                class="flex items-center gap-1 px-2 py-0.5 text-[11px] rounded transition-colors cursor-pointer"
                :class="store.mode === 'plan'
                  ? 'bg-amber-100 dark:bg-amber-500/15 text-amber-600 dark:text-amber-400'
                  : 'bg-zinc-100 dark:bg-zinc-700/60 text-zinc-500 dark:text-zinc-400 hover:text-zinc-700 dark:hover:text-zinc-200'"
                @click.stop="showModePicker = !showModePicker; showModelPicker = false"
              >
                {{ store.mode === 'agent' ? '⚡ Agent' : '📋 Plan' }}
                <svg class="w-3 h-3 opacity-50" viewBox="0 0 20 20" fill="currentColor">
                  <path fill-rule="evenodd" d="M5.23 7.21a.75.75 0 011.06.02L10 11.168l3.71-3.938a.75.75 0 111.08 1.04l-4.25 4.5a.75.75 0 01-1.08 0l-4.25-4.5a.75.75 0 01.02-1.06z" clip-rule="evenodd" />
                </svg>
              </button>
              <div v-if="showModePicker" class="absolute bottom-full mb-1 left-0 z-20 bg-white dark:bg-zinc-800 border border-zinc-200 dark:border-zinc-700 rounded-md shadow-lg dark:shadow-2xl py-1 min-w-28">
                <button
                  v-for="m in modes"
                  :key="m.value"
                  class="w-full px-3 py-1.5 text-xs cursor-pointer select-none text-left transition-colors rounded"
                  :class="store.mode === m.value
                    ? 'text-emerald-600 dark:text-emerald-400 bg-emerald-50 dark:bg-emerald-500/10'
                    : 'text-zinc-500 dark:text-zinc-400 hover:bg-zinc-50 dark:hover:bg-zinc-700/50 hover:text-zinc-700 dark:hover:text-zinc-200'"
                  @click="selectMode(m.value)"
                >
                  {{ m.icon }} {{ m.label }}
                </button>
              </div>
            </div>

            <!-- Model selector -->
            <div class="relative">
              <button
                class="flex items-center gap-1 px-2 py-0.5 text-[11px] rounded bg-zinc-100 dark:bg-zinc-700/60 text-zinc-500 dark:text-zinc-400 hover:text-zinc-700 dark:hover:text-zinc-200 cursor-pointer transition-colors"
                @click.stop="showModelPicker = !showModelPicker; showModePicker = false"
              >
                {{ store.modelName || 'model' }}
                <svg class="w-3 h-3 opacity-50" viewBox="0 0 20 20" fill="currentColor">
                  <path fill-rule="evenodd" d="M5.23 7.21a.75.75 0 011.06.02L10 11.168l3.71-3.938a.75.75 0 111.08 1.04l-4.25 4.5a.75.75 0 01-1.08 0l-4.25-4.5a.75.75 0 01.02-1.06z" clip-rule="evenodd" />
                </svg>
              </button>
              <div
                v-if="showModelPicker"
                class="absolute bottom-full mb-1 left-0 z-20 bg-white dark:bg-zinc-800 border border-zinc-200 dark:border-zinc-700 rounded-md shadow-lg dark:shadow-2xl py-1.5 max-h-72 overflow-y-auto min-w-56"
              >
                <template v-for="p in store.providers" :key="p.id">
                  <div class="px-3 py-1 text-[10px] text-zinc-400 dark:text-zinc-500 uppercase tracking-wider font-semibold sticky top-0 bg-white dark:bg-zinc-800">
                    {{ p.name }}
                  </div>
                  <button
                    v-for="m in p.models"
                    :key="m.id"
                    class="w-full px-3 py-1.5 text-xs text-left cursor-pointer select-none truncate transition-colors"
                    :class="store.providerName === p.id && store.modelName === m.id
                      ? 'text-emerald-600 dark:text-emerald-400 bg-emerald-50 dark:bg-emerald-500/10'
                      : 'text-zinc-500 dark:text-zinc-400 hover:bg-zinc-50 dark:hover:bg-zinc-700/50 hover:text-zinc-700 dark:hover:text-zinc-200'"
                    @click="selectModel(p.id, m.id)"
                  >
                    {{ m.name || m.id }}
                  </button>
                </template>
                <div v-if="store.providers.length === 0" class="px-3 py-2 text-xs text-zinc-400 dark:text-zinc-500">
                  No models available
                </div>
              </div>
            </div>

            <!-- Auto-approve toggle -->
            <button
              class="flex items-center gap-1 px-2 py-0.5 text-[11px] rounded transition-colors cursor-pointer"
              :class="store.autoApprove
                ? 'bg-emerald-100 dark:bg-emerald-500/15 text-emerald-600 dark:text-emerald-400 hover:bg-emerald-200 dark:hover:bg-emerald-500/25'
                : 'bg-zinc-100 dark:bg-zinc-700/60 text-zinc-400 dark:text-zinc-500 hover:text-zinc-600 dark:hover:text-zinc-300 hover:bg-zinc-200 dark:hover:bg-zinc-600/60'"
              :title="store.autoApprove ? 'Auto-approve ON' : 'Auto-approve OFF'"
              @click="store.setAutoApprove(!store.autoApprove)"
            >
              <svg class="w-3 h-3" viewBox="0 0 20 20" fill="currentColor">
                <path fill-rule="evenodd" d="M16.704 4.153a.75.75 0 01.143 1.052l-8 10.5a.75.75 0 01-1.127.075l-4.5-4.5a.75.75 0 011.06-1.06l3.894 3.893 7.48-9.817a.75.75 0 011.05-.143z" clip-rule="evenodd" />
              </svg>
              Auto
            </button>

            <!-- Channel toggle -->
            <button
              v-if="store.channelAvailable"
              class="flex items-center gap-1 px-2 py-0.5 text-[11px] rounded transition-colors cursor-pointer"
              :class="store.channelEnabled
                ? 'bg-emerald-100 dark:bg-emerald-500/15 text-emerald-600 dark:text-emerald-400 hover:bg-emerald-200 dark:hover:bg-emerald-500/25'
                : 'bg-zinc-100 dark:bg-zinc-700/60 text-zinc-400 dark:text-zinc-500 hover:text-zinc-600 dark:hover:text-zinc-300 hover:bg-zinc-200 dark:hover:bg-zinc-600/60'"
              :title="store.channelEnabled ? 'WeChat notifications ON' : 'WeChat notifications OFF'"
              @click="store.toggleChannel(!store.channelEnabled)"
            >
              <svg class="w-3 h-3" viewBox="0 0 20 20" fill="currentColor">
                <path d="M2 5a2 2 0 012-2h7a2 2 0 012 2v4a2 2 0 01-2 2H9l-3 3v-3H4a2 2 0 01-2-2V5z" />
                <path d="M15 7v2a4 4 0 01-4 4H9.828l-1.766 1.767A2 2 0 0011 16h2l3 3v-3h1a2 2 0 002-2V9a2 2 0 00-2-2h-2z" />
              </svg>
              WeChat
            </button>
          </div>

          <div class="flex items-center gap-2">
            <span v-if="store.tokenInfo" class="text-[10px] text-zinc-400 dark:text-zinc-500 font-mono">
              {{ store.tokenInfo.total_tokens.toLocaleString() }} tokens
              <template v-if="store.tokenPercentage > 0"> · {{ store.tokenPercentage }}%</template>
            </span>
            <!-- Stop button -->
            <button
              v-if="store.isRunning"
              class="w-7 h-7 flex items-center justify-center rounded-md bg-red-500 hover:bg-red-600 text-white transition-colors cursor-pointer shadow-sm"
              title="Stop agent (Esc)"
              @click="store.stopAgent()"
            >
              <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="currentColor">
                <rect x="6" y="6" width="12" height="12" rx="2" />
              </svg>
            </button>
            <!-- Send button -->
            <button
              v-else
              class="w-7 h-7 flex items-center justify-center rounded-md bg-emerald-500 hover:bg-emerald-600 text-white transition-colors disabled:opacity-30 disabled:cursor-not-allowed cursor-pointer shadow-sm"
              :disabled="!input.trim()"
              @click="send"
            >
              <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5">
                <path d="M5 12h14M12 5l7 7-7 7" />
              </svg>
            </button>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
