<script setup lang="ts">
import { computed, nextTick, ref } from "vue";

import WorkspaceState from "./WorkspaceState.vue";
import type { WorkspaceStateKind } from "./workspacePresentation";

export type InspectorTargetState = "ready" | "invalid" | "deleted" | "permission-denied" | "expired";

const props = withDefaults(defineProps<{
  open: boolean;
  title: string;
  description?: string;
  targetState?: InspectorTargetState;
  targetDescription?: string;
  dirty?: boolean;
  trigger?: HTMLElement | null;
}>(), {
  description: "",
  targetState: "ready",
  targetDescription: "",
  dirty: false,
  trigger: null,
});

const emit = defineEmits<{
  "update:open": [value: boolean];
  closePrevent: [];
  afterEnter: [];
  afterLeave: [];
}>();

const heading = ref<HTMLElement | null>(null);
const unavailableKind = computed<WorkspaceStateKind | null>(() => (
  props.targetState === "ready" ? null : props.targetState
));

function requestOpen(value: boolean) {
  if (!value && props.dirty) {
    emit("closePrevent");
    return;
  }
  emit("update:open", value);
}

function preventDefaultEvent(event: Event) {
  event.preventDefault();
}

async function focusHeading() {
  await nextTick();
  heading.value?.focus({ preventScroll: true });
  emit("afterEnter");
}

function restoreTrigger() {
  if (props.trigger?.isConnected) props.trigger.focus({ preventScroll: true });
  emit("afterLeave");
}
</script>

<template>
  <span
    v-if="open"
    class="workspace-inspector-push-anchor"
    hidden
  />
  <USlideover
    :open="open"
    class="workspace-inspector-surface"
    :description="description || title"
    :modal="dirty"
    :overlay="false"
    :dismissible="!dirty"
    :content="{
      onInteractOutside: preventDefaultEvent,
      onCloseAutoFocus: preventDefaultEvent,
    }"
    :close="{ 'aria-label': '关闭 Inspector' }"
    @update:open="requestOpen"
    @close:prevent="emit('closePrevent')"
    @after:enter="focusHeading"
    @after:leave="restoreTrigger"
  >
    <template #title>
      <h2
        ref="heading"
        class="workspace-inspector-title"
        tabindex="-1"
      >
        {{ title }}
      </h2>
    </template>
    <template #description>
      <p class="workspace-inspector-description">
        {{ description }}
      </p>
    </template>
    <template #actions>
      <slot name="actions" />
    </template>
    <template #body>
      <WorkspaceState
        v-if="unavailableKind"
        :kind="unavailableKind"
        :description="targetDescription || undefined"
      >
        <template
          v-if="$slots.recovery"
          #actions
        >
          <slot name="recovery" />
        </template>
      </WorkspaceState>
      <div
        v-else
        class="workspace-inspector-body"
      >
        <slot />
      </div>
    </template>
    <template
      v-if="$slots.footer"
      #footer
    >
      <div class="workspace-inspector-footer">
        <slot name="footer" />
      </div>
    </template>
  </USlideover>
</template>

<style>
.workspace-inspector-push-anchor { display: none; }

#main-content > .route-ui-boundary {
  transition: margin-right var(--co-motion-standard) var(--co-ease-out);
}

.workspace-inspector-surface {
  z-index: var(--co-z-overlay);
  top: var(--co-header-height);
  bottom: 0;
  width: min(var(--co-inspector-max-width), calc(100vw - var(--co-sidebar-rail-width)));
  height: auto;
  max-width: var(--co-inspector-max-width);
  max-height: calc(100dvh - var(--co-header-height));
  background: var(--co-bg-overlay);
  box-shadow: var(--co-shadow-overlay);
}

.workspace-inspector-title { margin: 0; font-size: 16px; line-height: 1.35; }
.workspace-inspector-description { margin: 0; overflow-wrap: anywhere; }
.workspace-inspector-body { display: grid; min-width: 0; gap: var(--co-space-4); }
.workspace-inspector-footer {
  display: flex;
  min-width: 0;
  flex-wrap: wrap;
  align-items: center;
  justify-content: flex-end;
  gap: var(--co-space-2);
}

@media (max-width: 1024px) {
  .workspace-inspector-surface { width: min(var(--co-inspector-width), calc(100vw - var(--co-sidebar-rail-width))); }
}

@media (min-width: 1181px) {
  #main-content:has(.workspace-inspector-push-anchor) > .route-ui-boundary {
    margin-right: var(--co-inspector-max-width);
  }
}

@media (prefers-reduced-motion: reduce) {
  #main-content > .route-ui-boundary { transition: none; }
}
</style>
