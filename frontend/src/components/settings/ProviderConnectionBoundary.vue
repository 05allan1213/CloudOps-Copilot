<script setup lang="ts">
import UFormField from "@nuxt/ui/components/FormField.vue";
import UInput from "@nuxt/ui/components/Input.vue";
import UInputNumber from "@nuxt/ui/components/InputNumber.vue";
import USwitch from "@nuxt/ui/components/Switch.vue";

import type { ProviderConfiguration } from "../../api/platform";

const props = defineProps<{
  modelValue: ProviderConfiguration;
  endpointLabel: string;
  endpointHelp: string;
  resultLabel: string;
  resultHelp: string;
  contextLabel: string;
  contextHelp: string;
}>();

const emit = defineEmits<{
  "update:modelValue": [value: ProviderConfiguration];
}>();

function update<K extends keyof ProviderConfiguration>(key: K, value: ProviderConfiguration[K]) {
  emit("update:modelValue", { ...props.modelValue, [key]: value });
}

function numberValue(value: unknown, fallback: number): number {
  const number = Number(value);
  return Number.isFinite(number) ? number : fallback;
}
</script>

<template>
  <section class="provider-connection-boundary">
    <header>
      <div>
        <strong>连接设置</strong>
        <p>这些字段由当前 Configuration Revision 契约支持。</p>
      </div>
      <USwitch
        :model-value="modelValue.enabled"
        :label="modelValue.enabled ? '已启用' : '已停用'"
        @update:model-value="update('enabled', Boolean($event))"
      />
    </header>

    <div class="provider-connection-fields">
      <UFormField
        :label="endpointLabel"
        :name="`providers.${modelValue.provider}.endpoint`"
        :help="endpointHelp"
        :data-field="`providers.${modelValue.provider}.endpoint`"
      >
        <UInput
          :model-value="modelValue.endpoint"
          type="url"
          autocomplete="off"
          spellcheck="false"
          class="provider-connection-control"
          @update:model-value="update('endpoint', String($event))"
        />
      </UFormField>

      <div class="provider-connection-pair">
        <UFormField
          label="请求超时（秒）"
          :name="`providers.${modelValue.provider}.timeout_ms`"
          help="单次 Provider 请求的客户端等待上限。"
          :data-field="`providers.${modelValue.provider}.timeout_ms`"
        >
          <UInputNumber
            :model-value="Math.max(1, Math.round(modelValue.timeout_ms / 1000))"
            :min="1"
            :max="60"
            :step="1"
            class="provider-connection-control"
            @update:model-value="update('timeout_ms', numberValue($event, 1) * 1000)"
          />
        </UFormField>
        <UFormField
          :label="resultLabel"
          :name="`providers.${modelValue.provider}.max_results`"
          :help="resultHelp"
          :data-field="`providers.${modelValue.provider}.max_results`"
        >
          <UInputNumber
            :model-value="modelValue.max_results"
            :min="1"
            :max="10000"
            :step="10"
            class="provider-connection-control"
            @update:model-value="update('max_results', numberValue($event, 1))"
          />
        </UFormField>
      </div>

      <UFormField
        :label="contextLabel"
        :name="`providers.${modelValue.provider}.context_link_base`"
        :help="contextHelp"
        :data-field="`providers.${modelValue.provider}.context_link_base`"
      >
        <UInput
          :model-value="modelValue.context_link_base"
          type="url"
          autocomplete="off"
          spellcheck="false"
          class="provider-connection-control"
          @update:model-value="update('context_link_base', String($event))"
        />
      </UFormField>
    </div>
  </section>
</template>

<style scoped>
.provider-connection-boundary { display: grid; min-width: 0; gap: var(--co-space-4); }
.provider-connection-boundary > header { display: flex; min-width: 0; align-items: flex-start; justify-content: space-between; gap: var(--co-space-3); padding-bottom: var(--co-space-3); border-bottom: 1px solid var(--co-border-default); }
.provider-connection-boundary strong { font-size: 14px; }
.provider-connection-boundary p { margin: 2px 0 0; color: var(--co-text-muted); font-size: 11px; }
.provider-connection-fields { display: grid; min-width: 0; gap: var(--co-space-4); }
.provider-connection-pair { display: grid; min-width: 0; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: var(--co-space-3); }
.provider-connection-control { width: 100%; }
@media (max-width: 560px) { .provider-connection-pair { grid-template-columns: 1fr; } }
</style>
