<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/composables/api'
import { setAuthToken } from '@/composables/authToken'

// Shown when the server is bound to a non-loopback host and requires a token
// (reported via /api/health auth_required). The user pastes the token printed in
// the server's startup banner; on success it is persisted and the app boots.
const emit = defineEmits<{ authed: [] }>()
const { t } = useI18n()

const token = ref('')
const showToken = ref(false)
const error = ref('')
const submitting = ref(false)

async function submit() {
  const candidate = token.value.trim()
  if (!candidate) {
    error.value = t('auth.required')
    return
  }
  submitting.value = true
  error.value = ''
  try {
    await api.authVerify(candidate) // skipAuth: a 401 surfaces here, not as expiry
    setAuthToken(candidate)
    emit('authed')
  } catch {
    error.value = t('auth.invalid')
  } finally {
    submitting.value = false
  }
}
</script>

<template>
  <div class="auth-gate" style="z-index: var(--z-modal)">
    <!-- Drag strip so the macOS desktop shell stays draggable behind this overlay. -->
    <div class="titlebar-drag" data-tauri-drag-region aria-hidden="true" />
    <div class="auth-card">
      <div class="auth-logo" aria-hidden="true">
        <span class="auth-dim">[</span><span class="auth-accent">J</span><span class="auth-fg">CODE</span><span class="auth-dim">]</span>
      </div>
      <div class="auth-title">{{ t('auth.title') }}</div>
      <div class="auth-msg">{{ t('auth.body') }}</div>
      <form class="auth-form" @submit.prevent="submit">
        <div class="auth-input-row">
          <input
            v-model="token"
            :type="showToken ? 'text' : 'password'"
            class="auth-input"
            :placeholder="t('auth.placeholder')"
            autocomplete="off"
            autofocus
          />
          <button type="button" class="auth-toggle" @click="showToken = !showToken">
            {{ showToken ? t('auth.hide') : t('auth.show') }}
          </button>
        </div>
        <div v-if="error" class="auth-error">{{ error }}</div>
        <button type="submit" class="auth-submit" :disabled="submitting">
          {{ submitting ? t('auth.verifying') : t('auth.submit') }}
        </button>
      </form>
    </div>
  </div>
</template>

<style scoped>
.auth-gate {
  position: fixed;
  inset: 0;
  display: grid;
  place-items: center;
  padding: 24px;
  background: var(--color-background);
}
.auth-card {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 10px;
  width: 100%;
  max-width: 400px;
  text-align: center;
  padding: 32px;
  background: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-2xl);
  box-shadow: var(--shadow-lg);
}
.auth-logo {
  font-family: var(--font-mono);
  font-size: 26px;
  font-weight: 700;
  margin-bottom: 6px;
  user-select: none;
}
.auth-dim { color: var(--color-muted-foreground); }
.auth-accent { color: var(--color-primary); }
.auth-fg { color: var(--color-foreground); }
.auth-title {
  font-size: 15px;
  font-weight: 600;
  color: var(--color-foreground);
}
.auth-msg {
  font-size: 13px;
  line-height: 1.6;
  color: var(--color-muted-foreground);
  margin-bottom: 6px;
}
.auth-form {
  width: 100%;
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.auth-input-row {
  display: flex;
  gap: 8px;
}
.auth-input {
  flex: 1;
  min-width: 0;
  padding: 9px 12px;
  font-size: 13px;
  font-family: var(--font-mono);
  color: var(--color-foreground);
  background: var(--color-background);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-lg);
  outline: none;
}
.auth-input:focus { border-color: var(--color-primary); }
.auth-toggle {
  padding: 0 12px;
  font-size: 12px;
  color: var(--color-muted-foreground);
  background: transparent;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-lg);
  cursor: pointer;
  white-space: nowrap;
}
.auth-error {
  font-size: 12px;
  color: var(--color-danger-fg, #dc2626);
  text-align: left;
}
.auth-submit {
  margin-top: 2px;
  padding: 9px 22px;
  border: none;
  border-radius: var(--radius-lg);
  background: var(--color-primary);
  color: var(--color-on-primary);
  font-size: 13px;
  font-weight: 500;
  cursor: pointer;
  transition: opacity 0.15s;
}
.auth-submit:hover:not(:disabled) { opacity: 0.9; }
.auth-submit:disabled { opacity: 0.6; cursor: default; }
</style>
