<script setup lang="ts">
export type MonitoringConfirmationKind = "authorize-once" | "authorize-definition" | "revoke";

export interface MonitoringConfirmation {
  kind: MonitoringConfirmationKind;
  title: string;
  description: string;
  target: string;
  effect: string;
  authority: string;
  confirmLabel: string;
}

defineProps<{
  saveOpen: boolean;
  saveTitle: string;
  saveDescription: string;
  saving: boolean;
  confirmation: MonitoringConfirmation | null;
  confirming: boolean;
}>();

const emit = defineEmits<{
  "update:saveOpen": [value: boolean];
  "update:saveTitle": [value: string];
  "update:saveDescription": [value: string];
  save: [];
  closeConfirmation: [];
  confirm: [];
}>();

function inputValue(event: Event): string {
  return (event.currentTarget as HTMLInputElement | HTMLTextAreaElement).value;
}
</script>

<template>
  <UModal
    :open="saveOpen"
    title="保存 Query Definition"
    description="保存当前成功执行的精确查询、Scope、边界与 revision 身份。"
    :dismissible="!saving"
    :close="!saving"
    :ui="{ content: 'monitoring-dialog', footer: 'shrink-0' }"
    @update:open="emit('update:saveOpen', $event)"
  >
    <template #body>
      <form
        id="monitoring-save-form"
        class="monitoring-dialog__form"
        @submit.prevent="emit('save')"
      >
        <label>
          <span>名称</span>
          <input
            :value="saveTitle"
            name="query-definition-title"
            autocomplete="off"
            required
            autofocus
            maxlength="128"
            @input="emit('update:saveTitle', inputValue($event))"
          >
        </label>
        <label>
          <span>说明</span>
          <textarea
            :value="saveDescription"
            name="query-definition-description"
            autocomplete="off"
            rows="3"
            maxlength="512"
            @input="emit('update:saveDescription', inputValue($event))"
          />
        </label>
      </form>
    </template>
    <template #footer>
      <div class="monitoring-dialog__actions">
        <UButton
          color="neutral"
          variant="outline"
          icon="i-lucide-arrow-left"
          label="取消"
          :disabled="saving"
          @click="emit('update:saveOpen', false)"
        />
        <UButton
          type="submit"
          form="monitoring-save-form"
          color="primary"
          icon="i-lucide-save"
          label="保存定义"
          :loading="saving"
          :disabled="!saveTitle.trim()"
        />
      </div>
    </template>
  </UModal>

  <UModal
    :open="Boolean(confirmation)"
    :title="confirmation?.title"
    :description="confirmation?.description"
    :dismissible="!confirming"
    :close="!confirming"
    :ui="{ content: 'monitoring-dialog', footer: 'shrink-0' }"
    @update:open="(value) => { if (!value) emit('closeConfirmation') }"
  >
    <template #body>
      <div
        v-if="confirmation"
        class="monitoring-dialog__consequence"
      >
        <UAlert
          :color="confirmation.kind === 'revoke' ? 'error' : 'warning'"
          variant="soft"
          :icon="confirmation.kind === 'revoke' ? 'i-lucide-ban' : 'i-lucide-shield-check'"
          :title="confirmation.title"
          :description="confirmation.effect"
        />
        <dl>
          <div><dt>Target</dt><dd>{{ confirmation.target }}</dd></div>
          <div><dt>Effect</dt><dd>{{ confirmation.effect }}</dd></div>
          <div><dt>Authority</dt><dd>{{ confirmation.authority }}</dd></div>
        </dl>
      </div>
    </template>
    <template #footer>
      <div class="monitoring-dialog__actions">
        <UButton
          color="neutral"
          variant="outline"
          icon="i-lucide-arrow-left"
          label="取消"
          :disabled="confirming"
          @click="emit('closeConfirmation')"
        />
        <UButton
          :color="confirmation?.kind === 'revoke' ? 'error' : 'primary'"
          :icon="confirmation?.kind === 'revoke' ? 'i-lucide-ban' : 'i-lucide-shield-check'"
          :label="confirmation?.confirmLabel"
          :loading="confirming"
          @click="emit('confirm')"
        />
      </div>
    </template>
  </UModal>
</template>

<style>
.monitoring-dialog { width: min(540px, calc(100vw - 32px)); }
.monitoring-dialog__form,
.monitoring-dialog__consequence { display: grid; min-width: 0; gap: var(--co-space-4); }
.monitoring-dialog__form label { display: grid; min-width: 0; gap: var(--co-space-1); }
.monitoring-dialog__form label span { color: var(--co-text-secondary); font-size: 12px; font-weight: 700; }
.monitoring-dialog__form input,
.monitoring-dialog__form textarea {
  width: 100%;
  border: 1px solid var(--co-border-default);
  border-radius: var(--co-radius-control);
  background: var(--co-bg-canvas);
  color: var(--co-text-primary);
}
.monitoring-dialog__form input { min-height: 38px; padding: var(--co-space-2) var(--co-space-3); }
.monitoring-dialog__form textarea { resize: vertical; padding: var(--co-space-3); line-height: 1.5; }
.monitoring-dialog__consequence dl { display: grid; margin: 0; }
.monitoring-dialog__consequence dl div { display: grid; grid-template-columns: 96px minmax(0, 1fr); gap: var(--co-space-3); padding: var(--co-space-2) 0; border-bottom: 1px solid var(--co-border-subtle); }
.monitoring-dialog__consequence dt { color: var(--co-text-muted); font-size: 11px; font-weight: 700; }
.monitoring-dialog__consequence dd { min-width: 0; margin: 0; overflow-wrap: anywhere; font-size: 12px; }
.monitoring-dialog__actions { display: flex; width: 100%; justify-content: flex-end; gap: var(--co-space-2); }
</style>
