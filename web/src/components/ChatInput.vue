<script setup lang="ts">
import { ref, nextTick, watch } from 'vue'
import { useChatStore } from '@/stores/chat'
import { Listbox, ListboxButton, ListboxOptions, ListboxOption } from '@headlessui/vue'

const store = useChatStore()
const input = ref('')
const textarea = ref<HTMLTextAreaElement | null>(null)
const showModelPicker = ref(false)

const modes = [
  { value: 'build' as const, label: 'Build' },
  { value: 'plan' as const, label: 'Plan' },
]

function autoResize() {
  const el = textarea.value
  if (!el) return
  el.style.height = 'auto'
  el.style.height = Math.min(el.scrollHeight, 160) + 'px'
}

function handleKeyDown(e: KeyboardEvent) {
  if (e.key === 'Enter' && !e.shiftKey) {
    e.preventDefault()
    send()
  }
}

async function send() {
  const text = input.value.trim()
  if (!text || store.isRunning) return
  input.value = ''
  await nextTick()
  autoResize()
  store.sendMessage(text)
}

function selectModel(provider: string, model: string) {
  showModelPicker.value = false
  store.switchModel(provider, model)
}

watch(() => store.isRunning, (running) => {
  if (!running) nextTick(() => textarea.value?.focus())
})
</script>

<template>
  <div class="border-t border-stone-200 bg-white px-5 py-3">
    <!-- Input area -->
    <div class="max-w-3xl mx-auto">
      <div class="bg-stone-50 border border-stone-200 rounded-xl px-3 py-2 focus-within:border-teal-400 transition-colors">
        <textarea
          ref="textarea"
          v-model="input"
          placeholder="Ask anything…"
          rows="1"
          class="w-full bg-transparent text-stone-700 text-sm resize-none outline-none placeholder-stone-400 min-h-[24px] max-h-[160px] leading-relaxed"
          @keydown="handleKeyDown"
          @input="autoResize"
        />
        <!-- Toolbar row -->
        <div class="flex items-center justify-between mt-1.5 pt-1.5 border-t border-stone-200/60">
          <div class="flex items-center gap-1.5">
            <!-- Mode selector -->
            <Listbox :model-value="store.mode" @update:model-value="store.switchMode">
              <div class="relative">
                <ListboxButton class="flex items-center gap-1 px-2 py-0.5 text-[11px] rounded-md bg-stone-100 text-stone-500 hover:text-stone-700 cursor-pointer transition-colors">
                  {{ store.mode === 'build' ? 'Build' : 'Plan' }}
                  <svg class="w-3 h-3 opacity-50" viewBox="0 0 20 20" fill="currentColor">
                    <path fill-rule="evenodd" d="M5.23 7.21a.75.75 0 011.06.02L10 11.168l3.71-3.938a.75.75 0 111.08 1.04l-4.25 4.5a.75.75 0 01-1.08 0l-4.25-4.5a.75.75 0 01.02-1.06z" clip-rule="evenodd" />
                  </svg>
                </ListboxButton>
                <ListboxOptions class="absolute bottom-full mb-1 left-0 z-20 bg-white border border-stone-200 rounded-lg shadow-lg py-1 min-w-24 focus:outline-none">
                  <ListboxOption
                    v-for="m in modes"
                    :key="m.value"
                    :value="m.value"
                    class="px-3 py-1.5 text-xs cursor-pointer select-none"
                    :class="store.mode === m.value ? 'text-teal-600 bg-teal-50' : 'text-stone-500 hover:bg-stone-50 hover:text-stone-700'"
                  >
                    {{ m.label }}
                  </ListboxOption>
                </ListboxOptions>
              </div>
            </Listbox>

            <!-- Model selector -->
            <div class="relative">
              <button
                class="flex items-center gap-1 px-2 py-0.5 text-[11px] rounded-md bg-stone-100 text-stone-500 hover:text-stone-700 cursor-pointer transition-colors"
                @click="showModelPicker = !showModelPicker"
              >
                {{ store.modelName || 'model' }}
                <svg class="w-3 h-3 opacity-50" viewBox="0 0 20 20" fill="currentColor">
                  <path fill-rule="evenodd" d="M5.23 7.21a.75.75 0 011.06.02L10 11.168l3.71-3.938a.75.75 0 111.08 1.04l-4.25 4.5a.75.75 0 01-1.08 0l-4.25-4.5a.75.75 0 01.02-1.06z" clip-rule="evenodd" />
                </svg>
              </button>
              <!-- Model dropdown -->
              <div
                v-if="showModelPicker"
                class="absolute bottom-full mb-1 left-0 z-20 bg-white border border-stone-200 rounded-lg shadow-lg py-1 max-h-72 overflow-y-auto min-w-56"
              >
                <template v-for="p in store.providers" :key="p.id">
                  <div class="px-3 py-1 text-[10px] text-stone-400 uppercase tracking-wider font-semibold sticky top-0 bg-white">
                    {{ p.name }}
                  </div>
                  <button
                    v-for="m in p.models"
                    :key="m.id"
                    class="w-full px-3 py-1.5 text-xs text-left cursor-pointer select-none truncate"
                    :class="store.providerName === p.id && store.modelName === m.id ? 'text-teal-600 bg-teal-50' : 'text-stone-500 hover:bg-stone-50 hover:text-stone-700'"
                    @click="selectModel(p.id, m.id)"
                  >
                    {{ m.name || m.id }}
                  </button>
                </template>
                <div v-if="store.providers.length === 0" class="px-3 py-2 text-xs text-stone-400">
                  No models available
                </div>
              </div>
            </div>

            <!-- Auto-approve toggle -->
            <button
              class="flex items-center gap-1 px-2 py-0.5 text-[11px] rounded-md transition-colors cursor-pointer"
              :class="store.autoApprove
                ? 'bg-teal-100 text-teal-600 hover:bg-teal-200'
                : 'bg-stone-100 text-stone-400 hover:text-stone-600 hover:bg-stone-200'"
              :title="store.autoApprove ? 'Auto-approve ON' : 'Auto-approve OFF'"
              @click="store.setAutoApprove(!store.autoApprove)"
            >
              <svg class="w-3 h-3" viewBox="0 0 20 20" fill="currentColor">
                <path fill-rule="evenodd" d="M16.704 4.153a.75.75 0 01.143 1.052l-8 10.5a.75.75 0 01-1.127.075l-4.5-4.5a.75.75 0 011.06-1.06l3.894 3.893 7.48-9.817a.75.75 0 011.05-.143z" clip-rule="evenodd" />
              </svg>
              Auto
            </button>
          </div>

          <div class="flex items-center gap-2">
            <span v-if="store.tokenInfo" class="text-[10px] text-stone-400">
              {{ store.tokenInfo.total_tokens.toLocaleString() }} tokens
              <template v-if="store.tokenPercentage > 0"> · {{ store.tokenPercentage }}%</template>
            </span>
            <button
              class="w-7 h-7 flex items-center justify-center rounded-lg bg-teal-500 hover:bg-teal-600 text-white transition-colors disabled:opacity-30 disabled:cursor-not-allowed cursor-pointer"
              :disabled="store.isRunning || !input.trim()"
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
