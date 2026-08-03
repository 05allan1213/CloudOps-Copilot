<script setup lang="ts">
import UAlert from "@nuxt/ui/components/Alert.vue";
import UFormField from "@nuxt/ui/components/FormField.vue";
import UInput from "@nuxt/ui/components/Input.vue";

import type { ProviderConfiguration } from "../../api/platform";
import ProviderConnectionBoundary from "./ProviderConnectionBoundary.vue";

const props = defineProps<{ modelValue: ProviderConfiguration }>();
const emit = defineEmits<{ "update:modelValue": [value: ProviderConfiguration] }>();

function updateModel(value: unknown) {
  emit("update:modelValue", { ...props.modelValue, model: String(value) });
}
</script>

<template>
  <div class="provider-type-settings">
    <UAlert
      color="info"
      variant="soft"
      icon="i-lucide-sparkles"
      title="LLM Provider"
      description="配置 Agent 使用的模型、兼容 API 入口与调用边界；API Key 永远通过 Secret 引用提供。"
    />
    <UFormField
      label="模型"
      name="providers.llm.model"
      help="服务端 Provider 能识别的精确模型名称。"
      data-field="providers.llm.model"
    >
      <UInput
        :model-value="modelValue.model"
        autocomplete="off"
        spellcheck="false"
        class="provider-connection-control"
        @update:model-value="updateModel"
      />
    </UFormField>
    <ProviderConnectionBoundary
      :model-value="modelValue"
      endpoint-label="LLM API Base URL"
      endpoint-help="OpenAI-compatible 或当前 Provider 的 API 根地址。"
      result-label="模型输出 Token 上限"
      result-help="单次 Agent 模型回答允许生成的最大 Token 数；运行时上限为 4096。"
      context-label="Provider Console 地址"
      context-help="用于打开模型或请求的外部管理上下文。"
      @update:model-value="emit('update:modelValue', $event)"
    />
    <UAlert
      color="neutral"
      variant="soft"
      icon="i-lucide-braces"
      title="能力由运行时探测"
      description="Tool Calling、Structured Output、Retry 策略和能力协商尚未进入当前可写 Schema；页面不会用静态 Switch 冒充支持。"
    />
  </div>
</template>

<style scoped>.provider-connection-control { width: 100%; }</style>
