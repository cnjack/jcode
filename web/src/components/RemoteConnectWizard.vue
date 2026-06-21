<script setup lang="ts">
import { ref, reactive, watch, computed } from 'vue'
import {
  Dialog,
  DialogPanel,
  TransitionRoot,
  TransitionChild,
} from '@headlessui/vue'
import {
  ServerIcon,
  CubeIcon,
  XMarkIcon,
  ArrowLeftIcon,
  FolderIcon,
  ArrowPathIcon,
  CheckIcon,
  ChevronRightIcon,
} from '@heroicons/vue/24/outline'
import { useChatStore } from '@/stores/chat'
import { useProjectStore } from '@/stores/project'
import { api } from '@/composables/api'
import type { RemoteMeta, SSHAlias, RemoteAuthMethod } from '@/types/api'

type Prefill = RemoteMeta & { loadTaskUuid?: string }

const props = defineProps<{
  open: boolean
  prefill?: Prefill | null
}>()

const emit = defineEmits<{
  close: []
  bound: []
}>()

const store = useChatStore()
const projectStore = useProjectStore()

type Step = 'method' | 'config' | 'connecting' | 'dir'
const step = ref<Step>('method')
const method = ref<'ssh' | 'docker'>('ssh')

const form = reactive({
  host: '',
  port: 22,
  user: 'root',
  authMethod: 'key' as RemoteAuthMethod,
  password: '',
  keyPath: '~/.ssh/id_rsa',
  passphrase: '',
})

const aliases = ref<SSHAlias[]>([])
const selectedAlias = ref('')

const error = ref('')
const connectionId = ref('')
const bound = ref(false)

// Directory picker state.
const currentDir = ref('')
const dirs = ref<string[]>([])
const dirLoading = ref(false)

// Save-as-alias.
const saveAlias = ref(false)
const aliasName = ref('')

const steps: { key: Step; label: string }[] = [
  { key: 'method', label: 'Choose method' },
  { key: 'config', label: 'Configure' },
  { key: 'connecting', label: 'Connecting' },
  { key: 'dir', label: 'Select directory' },
]
const stepIndex = computed(() => steps.findIndex((s) => s.key === step.value))

watch(() => props.open, (isOpen) => {
  if (!isOpen) return
  resetState()
  void loadAliases()
  if (props.prefill) {
    // A prefill that carries a known remote path is a *reconnect* (reopening a
    // remote workspace/task that was connected before). Since we never persist
    // the SSH secret, the link must be re-established — but for key/agent auth
    // (the common case) we can do it silently instead of making the user re-fill
    // the form. Fall back to the form only if the key isn't accepted.
    if (props.prefill.remotePath) {
      void autoReconnect(props.prefill)
    } else {
      applyPrefill(props.prefill)
    }
  }
})

function resetState() {
  step.value = 'method'
  method.value = 'ssh'
  form.host = ''
  form.port = 22
  form.user = 'root'
  form.authMethod = 'key'
  form.password = ''
  form.keyPath = '~/.ssh/id_rsa'
  form.passphrase = ''
  selectedAlias.value = ''
  error.value = ''
  connectionId.value = ''
  bound.value = false
  currentDir.value = ''
  dirs.value = []
  saveAlias.value = false
  aliasName.value = ''
}

function applyPrefill(p: Prefill) {
  // host may carry a port (e.g. "1.2.3.4:22"); split it out for the form.
  const colon = p.host.lastIndexOf(':')
  form.host = colon >= 0 ? p.host.slice(0, colon) : p.host
  form.port = p.port || 22
  form.user = p.user || 'root'
  // Jump straight to the config step for a reconnect.
  step.value = 'config'
}

// Seamless reconnect: try key/agent auth to the known remote path and bind
// straight to it (loading the task if one was requested), so reopening a remote
// workspace doesn't make the user walk the wizard again. Any failure (password
// auth, non-default key, passphrase, missing path) drops to the prefilled form.
async function autoReconnect(p: Prefill) {
  applyPrefill(p)
  step.value = 'connecting'
  try {
    const res = await api.remoteConnect({
      type: 'ssh',
      host: form.host.trim(),
      port: form.port || 22,
      user: form.user.trim() || 'root',
      auth_method: 'key',
      key_path: form.keyPath.trim(),
    })
    connectionId.value = res.connection_id
    currentDir.value = p.remotePath && p.remotePath !== '/' ? p.remotePath : res.remote_pwd
    await bindHere()
    // bindHere closes on success; if it failed, let the user pick a directory.
    if (!bound.value) {
      if (connectionId.value) {
        await listDir(currentDir.value)
        step.value = 'dir'
      } else {
        step.value = 'config'
      }
    }
  } catch {
    // Key/agent auth not accepted — fall back to the prefilled form (no scary
    // error; the user just chooses auth + connects manually).
    error.value = ''
    step.value = 'config'
  }
}

async function loadAliases() {
  try {
    const res = await api.sshList()
    aliases.value = res.aliases || []
  } catch {
    aliases.value = []
  }
}

function applyAlias(name: string) {
  selectedAlias.value = name
  const a = aliases.value.find((x) => x.name === name)
  if (!a) return
  // addr is "user@host" (host may include :port).
  const at = a.addr.indexOf('@')
  const user = at >= 0 ? a.addr.slice(0, at) : ''
  let host = at >= 0 ? a.addr.slice(at + 1) : a.addr
  const colon = host.lastIndexOf(':')
  if (colon >= 0) {
    form.port = parseInt(host.slice(colon + 1), 10) || 22
    host = host.slice(0, colon)
  }
  if (user) form.user = user
  form.host = host
}

function chooseMethod(m: 'ssh' | 'docker') {
  if (m === 'docker') return // disabled placeholder
  method.value = m
}

// discardConnection releases an established-but-unbound SSH connection so it
// doesn't linger in the backend's pending registry (up to its TTL).
async function discardConnection() {
  if (connectionId.value && !bound.value) {
    try { await api.remoteCancel(connectionId.value) } catch { /* ignore */ }
  }
  connectionId.value = ''
}

function backToConfig() {
  void discardConnection()
  step.value = 'config'
}

async function connect() {
  if (!form.host.trim()) {
    error.value = 'Host is required'
    return
  }
  // Drop any prior pending connection (e.g. user went back and reconnected).
  await discardConnection()
  error.value = ''
  step.value = 'connecting'
  try {
    const res = await api.remoteConnect({
      type: 'ssh',
      host: form.host.trim(),
      port: form.port || 22,
      user: form.user.trim() || 'root',
      auth_method: form.authMethod,
      password: form.authMethod === 'password' ? form.password : undefined,
      key_path: form.authMethod === 'key' ? form.keyPath.trim() : undefined,
      passphrase: form.authMethod === 'key' ? form.passphrase : undefined,
    })
    connectionId.value = res.connection_id
    await listDir(res.remote_pwd)
    step.value = 'dir'
  } catch (e: unknown) {
    error.value = e instanceof Error ? e.message : 'Connection failed'
    step.value = 'config'
  }
}

async function listDir(path: string) {
  if (!connectionId.value) return
  dirLoading.value = true
  error.value = ''
  try {
    const res = await api.remoteListDir(connectionId.value, path)
    currentDir.value = res.path
    dirs.value = res.dirs
  } catch (e: unknown) {
    error.value = e instanceof Error ? e.message : 'Failed to list directory'
  } finally {
    dirLoading.value = false
  }
}

function navigate(name: string) {
  if (name === '..') {
    const parts = currentDir.value.split('/')
    parts.pop()
    listDir(parts.join('/') || '/')
    return
  }
  const base = currentDir.value.endsWith('/') ? currentDir.value : currentDir.value + '/'
  listDir(base + name)
}

const binding = ref(false)
async function bindHere() {
  if (!connectionId.value || binding.value) return
  binding.value = true
  error.value = ''
  try {
    const res = await api.remoteBind(connectionId.value, currentDir.value)
    const proj = projectStore.upsertRemoteProject(res.label, {
      host: res.host,
      user: res.user,
      port: res.port,
      remotePath: res.remote_path,
    })
    projectStore.setActive(proj.id)
    bound.value = true
    connectionId.value = '' // ownership transferred; do not cancel on close

    if (saveAlias.value && aliasName.value.trim()) {
      const addr = `${res.user}@${res.host}`
      try {
        await api.remoteSaveAlias(aliasName.value.trim(), addr, res.remote_path)
      } catch (e: unknown) {
        console.error('Failed to save SSH alias:', e)
      }
    }

    await store.resetToWelcomeAfterSwitch()
    if (props.prefill?.loadTaskUuid) {
      await store.loadSession(props.prefill.loadTaskUuid)
    }
    emit('bound')
    emit('close')
  } catch (e: unknown) {
    error.value = e instanceof Error ? e.message : 'Failed to bind workspace'
  } finally {
    binding.value = false
  }
}

function close() {
  void discardConnection()
  emit('close')
}
</script>

<template>
  <TransitionRoot :show="open" as="template">
    <Dialog class="relative z-50" @close="close">
      <TransitionChild
        enter="ease-out duration-150" enter-from="opacity-0" enter-to="opacity-100"
        leave="ease-in duration-100" leave-from="opacity-100" leave-to="opacity-0"
      >
        <div class="fixed inset-0" style="background: var(--backdrop); backdrop-filter: blur(6px)" />
      </TransitionChild>

      <!-- Native title-bar drag strip — the blurred backdrop covers the shell's
           own drag band, so without this the macOS window can't be dragged while
           the wizard is open. Rendered only inside is-tauri-macos. -->
      <div class="titlebar-drag" data-tauri-drag-region aria-hidden="true" />

      <div class="fixed inset-0 flex items-center justify-center p-4">
        <TransitionChild
          enter="ease-out duration-150" enter-from="opacity-0 translate-y-2" enter-to="opacity-100 translate-y-0"
          leave="ease-in duration-100" leave-from="opacity-100 translate-y-0" leave-to="opacity-0 translate-y-2"
        >
          <DialogPanel class="rcw">
            <!-- Step rail -->
            <div class="rcw-rail">
              <div class="rcw-rail-title">Remote connect</div>
              <ol class="rcw-steps">
                <li
                  v-for="(s, i) in steps"
                  :key="s.key"
                  class="rcw-step"
                  :class="{ done: i < stepIndex, current: i === stepIndex }"
                >
                  <span class="rcw-step-dot">
                    <CheckIcon v-if="i < stepIndex" class="w-3 h-3" />
                    <span v-else>{{ i + 1 }}</span>
                  </span>
                  <span class="rcw-step-label">{{ s.label }}</span>
                </li>
              </ol>
            </div>

            <!-- Content -->
            <div class="rcw-body">
              <button class="rcw-close" @click="close"><XMarkIcon class="w-4 h-4" /></button>

              <!-- Step 1: method -->
              <template v-if="step === 'method'">
                <h3 class="rcw-h">Choose a connection method</h3>
                <p class="rcw-sub">Pick how to enter the remote workspace, then fill in the details.</p>
                <div class="rcw-methods">
                  <button class="rcw-method" :class="{ active: method === 'ssh' }" @click="chooseMethod('ssh')">
                    <ServerIcon class="w-5 h-5" />
                    <span class="rcw-method-name">SSH</span>
                    <span class="rcw-method-desc">Remote host</span>
                  </button>
                  <button class="rcw-method disabled" disabled title="Coming soon">
                    <CubeIcon class="w-5 h-5" />
                    <span class="rcw-method-name">Docker</span>
                    <span class="rcw-method-desc">Coming soon</span>
                  </button>
                </div>
                <div class="rcw-foot">
                  <span />
                  <button class="rcw-primary" @click="step = 'config'">Next <ChevronRightIcon class="w-3.5 h-3.5" /></button>
                </div>
              </template>

              <!-- Step 2: config -->
              <template v-else-if="step === 'config' || step === 'connecting'">
                <h3 class="rcw-h">SSH connection</h3>
                <p class="rcw-sub">Key/agent auth uses your local SSH keys; password &amp; passphrase are supported.</p>

                <div v-if="error" class="rcw-error">{{ error }}</div>

                <div v-if="aliases.length > 0" class="rcw-field">
                  <label>Saved alias (optional)</label>
                  <select :value="selectedAlias" class="rcw-input" @change="applyAlias(($event.target as HTMLSelectElement).value)">
                    <option value="">Don't use an alias</option>
                    <option v-for="a in aliases" :key="a.name" :value="a.name">{{ a.name }} — {{ a.addr }}</option>
                  </select>
                </div>

                <div class="rcw-row">
                  <div class="rcw-field grow">
                    <label>Host</label>
                    <input v-model="form.host" class="rcw-input" placeholder="1.2.3.4 or example.com" :disabled="step === 'connecting'" />
                  </div>
                  <div class="rcw-field port">
                    <label>Port</label>
                    <input v-model.number="form.port" type="number" class="rcw-input" :disabled="step === 'connecting'" />
                  </div>
                </div>

                <div class="rcw-row">
                  <div class="rcw-field grow">
                    <label>User</label>
                    <input v-model="form.user" class="rcw-input" placeholder="root" :disabled="step === 'connecting'" />
                  </div>
                  <div class="rcw-field">
                    <label>Auth</label>
                    <div class="rcw-seg">
                      <button :class="{ on: form.authMethod === 'password' }" :disabled="step === 'connecting'" @click="form.authMethod = 'password'">Password</button>
                      <button :class="{ on: form.authMethod === 'key' }" :disabled="step === 'connecting'" @click="form.authMethod = 'key'">Key</button>
                    </div>
                  </div>
                </div>

                <template v-if="form.authMethod === 'password'">
                  <div class="rcw-field">
                    <label>Password</label>
                    <input v-model="form.password" type="password" class="rcw-input" :disabled="step === 'connecting'" />
                  </div>
                </template>
                <template v-else>
                  <div class="rcw-field">
                    <label>Private key path</label>
                    <input v-model="form.keyPath" class="rcw-input mono" placeholder="~/.ssh/id_rsa" :disabled="step === 'connecting'" />
                  </div>
                  <div class="rcw-field">
                    <label>Passphrase (optional)</label>
                    <input v-model="form.passphrase" type="password" class="rcw-input" :disabled="step === 'connecting'" />
                  </div>
                </template>

                <div class="rcw-foot">
                  <button class="rcw-ghost" :disabled="step === 'connecting'" @click="step = 'method'">Back</button>
                  <button class="rcw-primary" :disabled="step === 'connecting'" @click="connect">
                    <ArrowPathIcon v-if="step === 'connecting'" class="w-3.5 h-3.5 spin" />
                    {{ step === 'connecting' ? 'Connecting…' : 'Connect' }}
                  </button>
                </div>
              </template>

              <!-- Step 4: directory -->
              <template v-else-if="step === 'dir'">
                <h3 class="rcw-h">Select a directory</h3>
                <p class="rcw-sub">Choose the working directory for this remote workspace.</p>

                <div v-if="error" class="rcw-error">{{ error }}</div>

                <div class="rcw-dirbar">
                  <button class="rcw-back" title="Back to config" @click="backToConfig"><ArrowLeftIcon class="w-3.5 h-3.5" /></button>
                  <span class="rcw-dir-path">{{ currentDir || '/' }}</span>
                </div>

                <div class="rcw-dirlist">
                  <div v-if="dirLoading" class="rcw-hint"><ArrowPathIcon class="w-3.5 h-3.5 spin" /> Loading…</div>
                  <template v-else>
                    <button
                      v-for="d in dirs"
                      :key="d"
                      class="rcw-dir-row"
                      @click="navigate(d)"
                    >
                      <FolderIcon class="w-3.5 h-3.5" />
                      <span>{{ d }}</span>
                    </button>
                    <div v-if="dirs.length === 0" class="rcw-hint">No sub-directories</div>
                  </template>
                </div>

                <label class="rcw-save">
                  <input v-model="saveAlias" type="checkbox" />
                  <span>Save as alias</span>
                  <input v-if="saveAlias" v-model="aliasName" class="rcw-input mini" placeholder="name" />
                </label>

                <div class="rcw-foot">
                  <button class="rcw-ghost" :disabled="binding" @click="close">Cancel</button>
                  <button class="rcw-primary" :disabled="binding" @click="bindHere">
                    <ArrowPathIcon v-if="binding" class="w-3.5 h-3.5 spin" />
                    Use this directory
                  </button>
                </div>
              </template>
            </div>
          </DialogPanel>
        </TransitionChild>
      </div>
    </Dialog>
  </TransitionRoot>
</template>

<style scoped>
.rcw {
  display: flex;
  width: 100%;
  max-width: 760px;
  height: 520px;
  max-height: 86vh;
  overflow: hidden;
  background: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-xl, 16px);
  box-shadow: var(--shadow-lg);
}

/* Rail */
.rcw-rail {
  width: 220px;
  flex-shrink: 0;
  padding: 22px 18px;
  background: var(--color-background);
  border-right: 1px solid var(--color-border);
}
.rcw-rail-title {
  font-size: 12px;
  font-weight: 600;
  color: var(--color-muted-foreground);
  margin-bottom: 22px;
}
.rcw-steps {
  display: flex;
  flex-direction: column;
  gap: 6px;
  list-style: none;
  padding: 0;
  margin: 0;
}
.rcw-step {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 9px 10px;
  border-radius: var(--radius-lg);
  color: var(--color-muted-foreground);
}
.rcw-step.current {
  background: var(--color-surface);
  color: var(--color-foreground);
}
.rcw-step-dot {
  display: grid;
  place-items: center;
  width: 24px;
  height: 24px;
  flex-shrink: 0;
  border-radius: 50%;
  border: 1px solid var(--color-border);
  font-size: 12px;
  font-weight: 600;
}
.rcw-step.current .rcw-step-dot {
  background: var(--color-foreground);
  color: var(--color-background);
  border-color: var(--color-foreground);
}
.rcw-step.done .rcw-step-dot {
  background: var(--color-success);
  border-color: var(--color-success);
  color: var(--color-on-primary);
}
.rcw-step-label {
  font-size: 13px;
}

/* Body */
.rcw-body {
  flex: 1;
  min-width: 0;
  position: relative;
  padding: 24px 26px;
  overflow-y: auto;
}
.rcw-close {
  position: absolute;
  top: 18px;
  right: 18px;
  border: none;
  background: transparent;
  color: var(--color-muted-foreground);
  cursor: pointer;
}
.rcw-close:hover {
  color: var(--color-foreground);
}
.rcw-h {
  font-size: 18px;
  font-weight: 600;
  color: var(--color-foreground);
}
.rcw-sub {
  margin-top: 6px;
  margin-bottom: 18px;
  font-size: 12.5px;
  color: var(--color-muted-foreground);
}
.rcw-error {
  margin-bottom: 14px;
  padding: 8px 12px;
  font-size: 12px;
  border-radius: var(--radius-lg);
  color: var(--color-error-fg, #b91c1c);
  background: var(--color-error-bg, rgba(220, 38, 38, 0.08));
  border: 1px solid var(--color-error-fg, rgba(220, 38, 38, 0.3));
  word-break: break-word;
}

/* Methods */
.rcw-methods {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 14px;
}
.rcw-method {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 10px;
  padding: 20px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-xl);
  background: var(--color-background);
  cursor: pointer;
  color: var(--color-foreground);
  transition: border-color 0.15s, background 0.15s;
}
.rcw-method:hover:not(.disabled) {
  border-color: color-mix(in srgb, var(--color-foreground) 30%, transparent);
}
.rcw-method.active {
  border-color: var(--color-foreground);
  background: var(--color-surface);
}
.rcw-method.disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
.rcw-method-name {
  font-size: 15px;
  font-weight: 600;
}
.rcw-method-desc {
  font-size: 12px;
  color: var(--color-muted-foreground);
}

/* Form */
.rcw-row {
  display: flex;
  gap: 12px;
}
.rcw-field {
  display: flex;
  flex-direction: column;
  gap: 5px;
  margin-bottom: 14px;
}
.rcw-field.grow {
  flex: 1;
  min-width: 0;
}
.rcw-field.port {
  width: 96px;
  flex-shrink: 0;
}
.rcw-field label {
  font-size: 12px;
  color: var(--color-muted-foreground);
}
.rcw-input {
  width: 100%;
  padding: 9px 11px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-lg);
  background: var(--color-background);
  color: var(--color-foreground);
  font-size: 13px;
  outline: none;
  transition: border-color 0.15s;
}
.rcw-input:focus {
  border-color: var(--color-primary);
}
.rcw-input.mono {
  font-family: var(--font-mono);
  font-size: 12px;
}
.rcw-input.mini {
  width: 120px;
  padding: 5px 9px;
  font-size: 12px;
}
.rcw-seg {
  display: flex;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-lg);
  overflow: hidden;
}
.rcw-seg button {
  padding: 8px 14px;
  border: none;
  background: var(--color-background);
  color: var(--color-muted-foreground);
  font-size: 12.5px;
  cursor: pointer;
}
.rcw-seg button.on {
  background: var(--color-foreground);
  color: var(--color-background);
}

/* Directory list */
.rcw-dirbar {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 10px;
  margin-bottom: 8px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-lg);
  background: var(--color-background);
}
.rcw-back {
  display: grid;
  place-items: center;
  border: none;
  background: transparent;
  color: var(--color-muted-foreground);
  cursor: pointer;
  flex-shrink: 0;
}
.rcw-back:hover {
  color: var(--color-foreground);
}
.rcw-dir-path {
  font-family: var(--font-mono);
  font-size: 12px;
  color: var(--color-foreground);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.rcw-dirlist {
  height: 190px;
  overflow-y: auto;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-lg);
  background: var(--color-background);
  padding: 4px;
}
.rcw-dir-row {
  display: flex;
  align-items: center;
  gap: 9px;
  width: 100%;
  padding: 8px 10px;
  border: none;
  background: transparent;
  border-radius: var(--radius-md);
  color: var(--color-foreground);
  font-size: 13px;
  cursor: pointer;
  text-align: left;
}
.rcw-dir-row:hover {
  background: var(--color-muted);
}
.rcw-dir-row svg {
  color: var(--color-muted-foreground);
  flex-shrink: 0;
}
.rcw-hint {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  padding: 24px;
  font-size: 12px;
  color: var(--color-muted-foreground);
}
.rcw-save {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 12px;
  font-size: 12.5px;
  color: var(--color-muted-foreground);
  cursor: pointer;
}

/* Footer */
.rcw-foot {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-top: 22px;
}
.rcw-primary {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 9px 18px;
  border: none;
  border-radius: var(--radius-lg);
  background: var(--color-primary);
  color: var(--color-on-primary);
  font-size: 13px;
  font-weight: 500;
  cursor: pointer;
  transition: opacity 0.15s;
}
.rcw-primary:hover:not(:disabled) {
  opacity: 0.9;
}
.rcw-primary:disabled {
  opacity: 0.6;
  cursor: not-allowed;
}
.rcw-ghost {
  padding: 9px 16px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-lg);
  background: transparent;
  color: var(--color-foreground);
  font-size: 13px;
  cursor: pointer;
}
.rcw-ghost:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
.spin {
  animation: rcw-spin 0.8s linear infinite;
}
@keyframes rcw-spin {
  to { transform: rotate(360deg); }
}
</style>
