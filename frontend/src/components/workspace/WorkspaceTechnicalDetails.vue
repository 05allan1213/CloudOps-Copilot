<script setup lang="ts">
import CopyFeedbackButton from "./CopyFeedbackButton.vue";

export interface TechnicalDetailField {
  label: string;
  value?: string | number | null;
  code?: boolean;
  copyValue?: string;
}

withDefaults(defineProps<{
  fields?: readonly TechnicalDetailField[];
  title?: string;
  description?: string;
  defaultOpen?: boolean;
}>(), {
  fields: () => [],
  title: "技术详情",
  description: "查看完整标识、时间与原始技术值",
  defaultOpen: false,
});
</script>

<template>
  <UCollapsible
    class="workspace-technical-details"
    :default-open="defaultOpen"
  >
    <template #default="{ open }">
      <UButton
        color="neutral"
        variant="ghost"
        block
        class="workspace-technical-details-trigger"
        icon="i-lucide-braces"
        :trailing-icon="open ? 'i-lucide-chevron-up' : 'i-lucide-chevron-down'"
        :aria-label="`${open ? '收起' : '展开'}${title}`"
      >
        <span>
          <strong>{{ title }}</strong>
          <small v-if="description">{{ description }}</small>
        </span>
      </UButton>
    </template>
    <template #content>
      <div class="workspace-technical-details-content">
        <dl v-if="fields.length">
          <div
            v-for="field in fields"
            :key="field.label"
          >
            <dt>{{ field.label }}</dt>
            <dd
              :class="{ 'is-code': field.code }"
              :translate="field.code ? 'no' : undefined"
            >
              {{ field.value ?? "未提供" }}
            </dd>
            <CopyFeedbackButton
              v-if="field.copyValue"
              :value="field.copyValue"
              :label="`复制 ${field.label}`"
              :success-label="`${field.label} 已复制`"
            />
          </div>
        </dl>
        <slot />
      </div>
    </template>
  </UCollapsible>
</template>

<style scoped>
.workspace-technical-details { min-width: 0; overflow: hidden; border: 1px solid var(--co-border-default); border-radius: var(--co-radius-frame); }
.workspace-technical-details-trigger { min-height: var(--co-control-height); justify-content: flex-start; border-radius: 0; text-align: left; }
.workspace-technical-details-trigger > span { display: grid; min-width: 0; flex: 1 1 auto; gap: 1px; }
.workspace-technical-details-trigger strong { font-size: 12px; }
.workspace-technical-details-trigger small { color: var(--co-text-muted); font-size: 10px; font-weight: 400; overflow-wrap: anywhere; }
.workspace-technical-details-content { min-width: 0; padding: 0 var(--co-space-3) var(--co-space-3); }
.workspace-technical-details dl { display: grid; margin: 0; }
.workspace-technical-details dl > div {
  display: grid;
  min-width: 0;
  grid-template-columns: minmax(112px, .34fr) minmax(0, 1fr) auto;
  align-items: center;
  gap: var(--co-space-2);
  padding: var(--co-space-2) 0;
  border-top: 1px solid var(--co-border-subtle);
}
.workspace-technical-details dt { color: var(--co-text-muted); font-size: 11px; }
.workspace-technical-details dd { min-width: 0; margin: 0; color: var(--co-text-secondary); font-size: 11px; overflow-wrap: anywhere; }
.workspace-technical-details dd.is-code { font-family: var(--co-font-mono); font-variant-numeric: tabular-nums; }

@media (max-width: 1024px) {
  .workspace-technical-details dl > div { grid-template-columns: minmax(0, 1fr) auto; }
  .workspace-technical-details dd { grid-column: 1; }
  .workspace-technical-details dt { grid-column: 1; }
}
</style>
