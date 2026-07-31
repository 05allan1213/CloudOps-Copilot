<script setup lang="ts">
import { computed } from "vue";

import {
  missingRiskConfirmationFacts,
  riskConfirmationDefinition,
  type RiskConfirmationFacts,
  type RiskConfirmationKind,
} from "./workspacePresentation";
import WorkspaceState from "./WorkspaceState.vue";

const props = defineProps<{
  open: boolean;
  kind: RiskConfirmationKind;
  facts: RiskConfirmationFacts;
}>();

const emit = defineEmits<{
  "update:open": [value: boolean];
  confirm: [];
}>();

const definition = computed(() => riskConfirmationDefinition(props.kind));
const missingFacts = computed(() => missingRiskConfirmationFacts(props.kind, props.facts));
const canConfirm = computed(() => missingFacts.value.length === 0);
const factRows = computed(() => [
  ["Target", props.facts.target],
  ["Effect", props.facts.effect],
  ["Authority", props.facts.authority],
  ["Exact hash", props.facts.exactHash],
  ["Version", props.facts.version],
  ["不可逆后果", props.facts.irreversible],
  ["恢复限制", props.facts.recovery],
].filter((row): row is [string, string] => Boolean(row[1])));

function requestOpen(value: boolean) {
  if (!value) emit("update:open", false);
}
</script>

<template>
  <UModal
    :open="open"
    :title="definition.title"
    description="确认内容按真实后果分级；前端不会补造 authority、hash 或恢复保证。"
    :dismissible="definition.dismissible"
    :close="false"
    :ui="{
      overlay: 'risk-confirmation-overlay',
      content: 'risk-confirmation-surface',
      header: 'shrink-0 items-start',
      body: 'min-h-0 overflow-y-auto',
      footer: 'shrink-0',
    }"
    @update:open="requestOpen"
  >
    <template #body>
      <div class="risk-confirmation-body">
        <UAlert
          :color="definition.color === 'primary' ? 'info' : definition.color"
          variant="soft"
          :icon="definition.icon"
          :title="definition.title"
          :description="facts.effect"
        />
        <dl>
          <div
            v-for="row in factRows"
            :key="row[0]"
          >
            <dt>{{ row[0] }}</dt><dd>{{ row[1] }}</dd>
          </div>
        </dl>
        <WorkspaceState
          v-if="missingFacts.length"
          kind="error"
          title="关键身份缺失，操作保持阻止"
          :description="`缺少：${missingFacts.join(', ')}`"
        />
      </div>
    </template>
    <template #footer>
      <div class="risk-confirmation-actions">
        <UButton
          color="neutral"
          variant="outline"
          icon="i-lucide-arrow-left"
          label="取消"
          @click="emit('update:open', false)"
        />
        <UButton
          :color="definition.color"
          :icon="definition.icon"
          :label="definition.confirmLabel"
          :disabled="!canConfirm"
          @click="emit('confirm')"
        />
      </div>
    </template>
  </UModal>
</template>

<style>
.risk-confirmation-overlay { z-index: var(--co-z-overlay); }
.risk-confirmation-surface {
  z-index: calc(var(--co-z-overlay) + 1);
  width: min(var(--co-confirmation-width), calc(100vw - 32px));
}
.risk-confirmation-body { display: grid; min-width: 0; gap: var(--co-space-4); }
.risk-confirmation-body dl { display: grid; margin: 0; gap: var(--co-space-1); }
.risk-confirmation-body dl div {
  display: grid;
  grid-template-columns: 132px minmax(0, 1fr);
  gap: var(--co-space-2);
  padding: var(--co-space-2) 0;
  border-bottom: 1px solid var(--co-border-default);
}
.risk-confirmation-body dt {
  min-width: 0;
  color: var(--co-text-muted);
  overflow-wrap: anywhere;
}
.risk-confirmation-body dd { min-width: 0; margin: 0; overflow-wrap: anywhere; }
.risk-confirmation-actions { display: flex; width: 100%; justify-content: flex-end; gap: var(--co-space-2); }
</style>
