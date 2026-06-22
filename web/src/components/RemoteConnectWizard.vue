<script setup lang="ts">
import { ref, reactive, watch, computed } from 'vue'
import {
  Dialog,
  DialogPanel,
  TransitionRoot,
  TransitionChild,
  Listbox,
  ListboxButton,
  ListboxOptions,
  ListboxOption,
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
  ChevronUpDownIcon,
} from '@heroicons/vue/24/outline'
import { useI18n } from 'vue-i18n'
import { useChatStore } from '@/stores/chat'
import { useProjectStore } from '@/stores/project'
import { api } from '@/composables/api'
import type { RemoteMeta, SSHAlias, RemoteAuthMethod, DockerContainer } from '@/types/api'

type Prefill = RemoteMeta & { loadTaskUuid?: string }

const { t } = useI18n()

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

type Step = 'method' | 'config' | 'docker' | 'connecting' | 'dir'
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

// Docker container picker state.
const containers = ref<DockerContainer[]>([])
const containersLoading = ref(false)
const selectedContainer = ref('')

// Label shown on the alias Listbox button (mirrors the option text).
const selectedAliasLabel = computed(() => {
  const a = aliases.value.find((x) => x.name === selectedAlias.value)
  return a ? `${a.name} — ${a.addr}` : t('settings.ssh.noAlias')
})

const error = ref('')
const connectionId = ref('')
const bound = ref(false)

// Directory picker state.
const currentDir = ref('')
const dirs = ref<string[]>([])
const dirLoading = ref(false)

// Optional custom name for the saved alias (defaults to user@host on bind).
const aliasName = ref('')

const steps = computed<{ key: Step; label: string }[]>(() => [
  { key: 'method', label: t('wizard.steps.chooseMethod') },
  { key: 'config', label: t('wizard.steps.configure') },
  { key: 'connecting', label: t('wizard.steps.connecting') },
  { key: 'dir', label: t('wizard.steps.selectDirectory') },
])
const stepIndex = computed(() => {
  // The docker container picker occupies the same rail position as SSH config.
  const key = step.value === 'docker' ? 'config' : step.value
  return steps.value.findIndex((s) => s.key === key)
})

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
    if (props.prefill.kind === 'docker') {
      void autoReconnectDocker(props.prefill)
    } else if (props.prefill.remotePath) {
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
  containers.value = []
  selectedContainer.value = ''
  error.value = ''
  connectionId.value = ''
  bound.value = false
  currentDir.value = ''
  dirs.value = []
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
  method.value = m
}

function proceedFromMethod() {
  if (method.value === 'docker') {
    step.value = 'docker'
    void loadContainers()
  } else {
    step.value = 'config'
  }
}

async function loadContainers() {
  containersLoading.value = true
  error.value = ''
  try {
    const res = await api.dockerContainers()
    // Running containers first, then by name.
    containers.value = (res.containers || []).slice().sort((a, b) => {
      if (a.running !== b.running) return a.running ? -1 : 1
      return (a.name || a.id).localeCompare(b.name || b.id)
    })
  } catch (e: unknown) {
    error.value = e instanceof Error ? e.message : 'Failed to list containers'
    containers.value = []
  } finally {
    containersLoading.value = false
  }
}

async function connectDocker(container: string) {
  if (!container) return
  await discardConnection()
  selectedContainer.value = container
  error.value = ''
  step.value = 'connecting'
  try {
    const res = await api.remoteConnect({ type: 'docker', container })
    connectionId.value = res.connection_id
    await listDir(res.remote_pwd)
    step.value = 'dir'
  } catch (e: unknown) {
    error.value = e instanceof Error ? e.message : 'Connection failed'
    step.value = 'docker'
  }
}

// Seamless docker reconnect: re-attach to the known container and bind straight
// to its previous path. Falls back to the container picker on any failure.
async function autoReconnectDocker(p: Prefill) {
  method.value = 'docker'
  if (!p.container) {
    step.value = 'docker'
    void loadContainers()
    return
  }
  step.value = 'connecting'
  try {
    const res = await api.remoteConnect({ type: 'docker', container: p.container })
    connectionId.value = res.connection_id
    currentDir.value = p.remotePath && p.remotePath !== '/' ? p.remotePath : res.remote_pwd
    await bindHere()
    if (!bound.value && connectionId.value) {
      await listDir(currentDir.value)
      step.value = 'dir'
    }
  } catch {
    error.value = ''
    step.value = 'docker'
    void loadContainers()
  }
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
    const isDocker = res.kind === 'docker'
    const proj = projectStore.upsertRemoteProject(res.label, isDocker
      ? { kind: 'docker', host: '', user: '', port: 0, remotePath: res.remote_path, container: res.container }
      : { kind: 'ssh', host: res.host, user: res.user, port: res.port, remotePath: res.remote_path })
    projectStore.setActive(proj.id)
    bound.value = true
    connectionId.value = '' // ownership transferred; do not cancel on close

    // Always persist the target to config so it returns for one-click reconnects
    // next time — no opt-in needed. Use the custom name when given, otherwise
    // derive a stable one. Secrets are never stored.
    if (isDocker) {
      const container = res.container || ''
      const name = aliasName.value.trim() || container || 'container'
      try {
        await api.remoteSaveDockerAlias(name, container, res.remote_path)
      } catch (e: unknown) {
        console.error('Failed to save Docker alias:', e)
      }
    } else {
      const addr = `${res.user}@${res.host}`
      const name = aliasName.value.trim() || addr
      try {
        await api.remoteSaveAlias(name, addr, res.remote_path)
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
              <div class="rcw-rail-title">{{ t('wizard.title') }}</div>
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
                <h3 class="rcw-h">{{ t('wizard.chooseMethod') }}</h3>
                <p class="rcw-sub">{{ t('wizard.pickMethodDesc') }}</p>
                <div class="rcw-methods">
                  <button class="rcw-method" :class="{ active: method === 'ssh' }" @click="chooseMethod('ssh')">
                    <ServerIcon class="w-5 h-5" />
                    <span class="rcw-method-name">{{ t('wizard.ssh') }}</span>
                    <span class="rcw-method-desc">{{ t('wizard.remoteHost') }}</span>
                  </button>
                  <button class="rcw-method" :class="{ active: method === 'docker' }" @click="chooseMethod('docker')">
                    <CubeIcon class="w-5 h-5" />
                    <span class="rcw-method-name">{{ t('wizard.docker') }}</span>
                    <span class="rcw-method-desc">{{ t('wizard.container') }}</span>
                  </button>
                </div>
                <div class="rcw-foot">
                  <span />
                  <button class="rcw-primary" @click="proceedFromMethod">{{ t('wizard.next') }} <ChevronRightIcon class="w-3.5 h-3.5" /></button>
                </div>
              </template>

              <!-- Step 2: config (SSH) -->
              <template v-else-if="step === 'config' || (step === 'connecting' && method !== 'docker')">
                <h3 class="rcw-h">{{ t('wizard.sshConnection') }}</h3>
                <p class="rcw-sub">{{ t('wizard.sshDesc') }}</p>

                <div v-if="error" class="rcw-error">{{ error }}</div>

                <div v-if="aliases.length > 0" class="rcw-field">
                  <label>{{ t('settings.ssh.savedAlias') }}</label>
                  <Listbox
                    :model-value="selectedAlias"
                    :disabled="step === 'connecting'"
                    @update:model-value="applyAlias"
                  >
                    <div class="rcw-listbox">
                      <ListboxButton class="rcw-input rcw-select">
                        <span class="rcw-select-label" :class="{ placeholder: !selectedAlias }">{{ selectedAliasLabel }}</span>
                        <ChevronUpDownIcon class="rcw-select-chevron" />
                      </ListboxButton>
                      <transition
                        enter-active-class="pop-enter-active"
                        enter-from-class="pop-enter-from"
                        leave-active-class="pop-leave-active"
                        leave-to-class="pop-leave-to"
                      >
                        <ListboxOptions class="rcw-options">
                          <ListboxOption v-slot="{ active, selected }" :value="''" as="template">
                            <li class="rcw-option" :class="{ active }">
                              <span class="rcw-option-text">{{ t('settings.ssh.noAlias') }}</span>
                              <CheckIcon v-if="selected" class="rcw-option-check" />
                            </li>
                          </ListboxOption>
                          <ListboxOption
                            v-for="a in aliases"
                            :key="a.name"
                            v-slot="{ active, selected }"
                            :value="a.name"
                            as="template"
                          >
                            <li class="rcw-option" :class="{ active }">
                              <span class="rcw-option-text">{{ a.name }} — {{ a.addr }}</span>
                              <CheckIcon v-if="selected" class="rcw-option-check" />
                            </li>
                          </ListboxOption>
                        </ListboxOptions>
                      </transition>
                    </div>
                  </Listbox>
                </div>

                <div class="rcw-row">
                  <div class="rcw-field grow">
                    <label>{{ t('settings.ssh.host') }}</label>
                    <input v-model="form.host" class="rcw-input" :placeholder="t('wizard.hostPlaceholder')" :disabled="step === 'connecting'" />
                  </div>
                  <div class="rcw-field port">
                    <label>{{ t('settings.ssh.port') }}</label>
                    <input v-model.number="form.port" type="number" class="rcw-input" :disabled="step === 'connecting'" />
                  </div>
                </div>

                <div class="rcw-row">
                  <div class="rcw-field grow">
                    <label>{{ t('settings.ssh.user') }}</label>
                    <input v-model="form.user" class="rcw-input" :placeholder="t('wizard.userPlaceholder')" :disabled="step === 'connecting'" />
                  </div>
                  <div class="rcw-field">
                    <label>{{ t('wizard.auth') }}</label>
                    <div class="rcw-seg">
                      <button :class="{ on: form.authMethod === 'password' }" :disabled="step === 'connecting'" @click="form.authMethod = 'password'">{{ t('settings.ssh.password') }}</button>
                      <button :class="{ on: form.authMethod === 'key' }" :disabled="step === 'connecting'" @click="form.authMethod = 'key'">{{ t('settings.ssh.key') }}</button>
                    </div>
                  </div>
                </div>

                <template v-if="form.authMethod === 'password'">
                  <div class="rcw-field">
                    <label>{{ t('settings.ssh.password') }}</label>
                    <input v-model="form.password" type="password" class="rcw-input" :disabled="step === 'connecting'" />
                  </div>
                </template>
                <template v-else>
                  <div class="rcw-field">
                    <label>{{ t('settings.ssh.privateKeyPath') }}</label>
                    <input v-model="form.keyPath" class="rcw-input mono" placeholder="~/.ssh/id_rsa" :disabled="step === 'connecting'" />
                  </div>
                  <div class="rcw-field">
                    <label>{{ t('settings.ssh.passphrase') }}</label>
                    <input v-model="form.passphrase" type="password" class="rcw-input" :disabled="step === 'connecting'" />
                  </div>
                </template>

                <div class="rcw-foot">
                  <button class="rcw-ghost" :disabled="step === 'connecting'" @click="step = 'method'">{{ t('common.back') }}</button>
                  <button class="rcw-primary" :disabled="step === 'connecting'" @click="connect">
                    <ArrowPathIcon v-if="step === 'connecting'" class="w-3.5 h-3.5 spin" />
                    {{ step === 'connecting' ? t('wizard.connecting') : t('wizard.connect') }}
                  </button>
                </div>
              </template>

              <!-- Step 2b: docker container picker -->
              <template v-else-if="step === 'docker' || (step === 'connecting' && method === 'docker')">
                <h3 class="rcw-h">{{ t('wizard.dockerConnection') }}</h3>
                <p class="rcw-sub">{{ t('wizard.dockerDesc') }}</p>

                <div v-if="error" class="rcw-error">{{ error }}</div>

                <div v-if="step === 'connecting'" class="rcw-hint">
                  <ArrowPathIcon class="w-3.5 h-3.5 spin" /> {{ t('wizard.connecting') }}
                </div>
                <template v-else>
                  <div class="rcw-dirbar">
                    <span class="rcw-dir-path">{{ t('wizard.selectContainer') }}</span>
                    <button class="rcw-back" style="margin-left:auto" :title="t('wizard.refresh')" @click="loadContainers">
                      <ArrowPathIcon class="w-3.5 h-3.5" :class="{ spin: containersLoading }" />
                    </button>
                  </div>

                  <div class="rcw-dirlist">
                    <div v-if="containersLoading" class="rcw-hint"><ArrowPathIcon class="w-3.5 h-3.5 spin" /> {{ t('wizard.loading') }}</div>
                    <template v-else>
                      <button
                        v-for="c in containers"
                        :key="c.id"
                        class="rcw-dir-row"
                        @click="connectDocker(c.name || c.id)"
                      >
                        <CubeIcon class="w-3.5 h-3.5" />
                        <span class="rcw-ctr-name">{{ c.name || c.id.slice(0, 12) }}</span>
                        <span class="rcw-ctr-img">{{ c.image }}</span>
                        <span class="rcw-ctr-state" :class="{ on: c.running }">{{ c.running ? t('wizard.running') : t('wizard.stopped') }}</span>
                      </button>
                      <div v-if="containers.length === 0" class="rcw-hint">{{ t('wizard.noContainers') }}</div>
                    </template>
                  </div>

                  <div class="rcw-foot">
                    <button class="rcw-ghost" @click="step = 'method'">{{ t('common.back') }}</button>
                    <span />
                  </div>
                </template>
              </template>

              <!-- Step 4: directory -->
              <template v-else-if="step === 'dir'">
                <h3 class="rcw-h">{{ t('wizard.selectDirectory') }}</h3>
                <p class="rcw-sub">{{ t('wizard.dirDesc') }}</p>

                <div v-if="error" class="rcw-error">{{ error }}</div>

                <div class="rcw-dirbar">
                  <button class="rcw-back" :title="t('wizard.backToConfig')" @click="backToConfig"><ArrowLeftIcon class="w-3.5 h-3.5" /></button>
                  <span class="rcw-dir-path">{{ currentDir || '/' }}</span>
                </div>

                <div class="rcw-dirlist">
                  <div v-if="dirLoading" class="rcw-hint"><ArrowPathIcon class="w-3.5 h-3.5 spin" /> {{ t('wizard.loading') }}</div>
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
                    <div v-if="dirs.length === 0" class="rcw-hint">{{ t('wizard.noSubDirs') }}</div>
                  </template>
                </div>

                <label class="rcw-save">
                  <span>{{ t('wizard.saveAlias') }}</span>
                  <input v-model="aliasName" class="rcw-input mini" :placeholder="t('wizard.aliasNamePlaceholder')" />
                </label>

                <div class="rcw-foot">
                  <button class="rcw-ghost" :disabled="binding" @click="close">{{ t('common.cancel') }}</button>
                  <button class="rcw-primary" :disabled="binding" @click="bindHere">
                    <ArrowPathIcon v-if="binding" class="w-3.5 h-3.5 spin" />
                    {{ t('wizard.useDirectory') }}
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
  width: 900px;
  max-width: calc(100vw - 32px);
  height: 600px;
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
  color: var(--color-surface);
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
  border-color: var(--color-accent-neutral);
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

/* Alias picker — Headless UI Listbox styled to match .rcw-input fields. */
.rcw-listbox {
  position: relative;
}
.rcw-select {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  text-align: left;
  font-family: inherit;
  cursor: pointer;
}
.rcw-select:focus,
.rcw-select:focus-visible {
  border-color: var(--color-accent-neutral);
}
.rcw-select-label {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.rcw-select-label.placeholder {
  color: var(--color-muted-foreground);
}
.rcw-select-chevron {
  width: 16px;
  height: 16px;
  flex-shrink: 0;
  color: var(--color-muted-foreground);
}
.rcw-options {
  position: absolute;
  z-index: var(--z-dropdown);
  top: calc(100% + 4px);
  left: 0;
  right: 0;
  max-height: 240px;
  overflow-y: auto;
  margin: 0;
  padding: 4px;
  list-style: none;
  background: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-lg);
  box-shadow: var(--shadow-md);
  outline: none;
}
.rcw-option {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 7px 9px;
  border-radius: var(--radius-md);
  font-size: 13px;
  color: var(--color-foreground);
  cursor: pointer;
}
.rcw-option.active {
  background: var(--color-secondary);
}
.rcw-option-text {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.rcw-option-check {
  width: 15px;
  height: 15px;
  flex-shrink: 0;
  color: var(--color-accent);
}
.pop-enter-active,
.pop-leave-active {
  transition: opacity 0.12s ease, transform 0.12s ease;
}
.pop-enter-from,
.pop-leave-to {
  opacity: 0;
  transform: translateY(-4px);
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
.rcw-ctr-name {
  font-weight: 500;
  flex-shrink: 0;
}
.rcw-ctr-img {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 12px;
  color: var(--color-muted-foreground);
}
.rcw-ctr-state {
  flex-shrink: 0;
  font-size: 11px;
  padding: 2px 7px;
  border-radius: 999px;
  background: var(--color-muted);
  color: var(--color-muted-foreground);
}
.rcw-ctr-state.on {
  background: color-mix(in srgb, var(--color-success) 18%, transparent);
  color: var(--color-success);
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
  background: var(--color-accent-neutral);
  color: var(--color-surface);
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
