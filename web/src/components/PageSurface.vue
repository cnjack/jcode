<script setup lang="ts">
// Shared page surface for secondary pages (Automations, Channels) that render
// inside <main> as a wrapped inset panel — the same surface geometry the chat
// canvas uses (border / radius / margin / shadow), so the page reads as a panel
// within the shell (包裹感) rather than a full-bleed overlay.
//
// Centralizing the chrome here removes the duplicate .auto-panel / .chan-panel
// copies that had drifted out of sync. Each page supplies a title and optional
// #actions (e.g. the Automations segmented control + primary button); the
// surface owns no close button — page dismissal is via Esc / the nav header /
// clicking a task, not an in-page X.
// colWidth lets each page set its own content-column width (e.g. 48rem for
// Automations, 56rem for Channels). The head aligns to the same column so the
// title + actions line up with the body content rather than hugging the panel
// edges. Exposed as the --page-col custom property below.
defineProps<{ title: string; colWidth?: string }>()
</script>

<template>
  <div class="page-surface" :style="colWidth ? { '--page-col': colWidth } : undefined">
    <header class="page-head">
      <h1>{{ title }}</h1>
      <div v-if="$slots.actions" class="page-actions">
        <slot name="actions" />
      </div>
    </header>
    <div class="page-body">
      <slot />
    </div>
  </div>
</template>

<style scoped>
.page-surface {
  display: flex;
  flex-direction: column;
  min-height: 0;
  background: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-2xl);
  /* Mirrors .chat-panel's margin so secondary pages align with the chat canvas.
     The macOS Tauri override (style.css) collapses the top to 20px since the
     28px title-bar strip already provides clearance. */
  margin: 48px 14px 14px;
  box-shadow: var(--shadow-sm);
  overflow: hidden;
  color: var(--color-foreground);
}
.page-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  /* Collapse the head into the same centered column as the body content
     (:deep(.page-body) > * in each page) so the title + actions align with the
     cards/runs below instead of stretching to the panel edges. */
  width: 100%;
  max-width: var(--page-col, 48rem);
  margin-inline: auto;
  padding: 16px 20px 12px;
  flex-shrink: 0;
}
.page-head h1 {
  font-size: 17px;
  font-weight: 600;
}
.page-actions {
  display: flex;
  align-items: center;
  gap: 8px;
}
.page-body {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  /* Flex column so a page's empty-state can fill the body height and centre its
     hero vertically (see .col.center-empty in AutomationsView). Pages with real
     content still stack from the top since their children default to auto. */
  display: flex;
  flex-direction: column;
}
</style>
