<script setup lang="ts">
import UAlert from "@nuxt/ui/components/Alert.vue";

import type { ProviderConfiguration } from "../../api/platform";
import ProviderConnectionBoundary from "./ProviderConnectionBoundary.vue";

defineProps<{ modelValue: ProviderConfiguration }>();
const emit = defineEmits<{ "update:modelValue": [value: ProviderConfiguration] }>();
</script>

<template>
  <div class="provider-type-settings">
    <UAlert
      color="info"
      variant="soft"
      icon="i-lucide-github"
      title="GitHub 仓库连接"
      description="配置 GitHub API 入口与仓库上下文链接；写入能力仍按 Operation authority 单独控制。"
    />
    <ProviderConnectionBoundary
      :model-value="modelValue"
      endpoint-label="GitHub API Base URL"
      endpoint-help="GitHub.com 或 GitHub Enterprise API 根地址。"
      result-label="仓库结果上限"
      result-help="单次仓库、分支或提交读取的最大数量。"
      context-label="组织或仓库地址"
      context-help="用于从 CloudOps 打开可信的 Repository 上下文。"
      @update:model-value="emit('update:modelValue', $event)"
    />
    <UAlert
      color="neutral"
      variant="soft"
      icon="i-lucide-app-window"
      title="GitHub App 身份"
      description="Installation ID、Repository allowlist、Branch 策略与读写权限尚未进入当前前端 Schema；Secret 仅通过独立引用管理。"
    />
  </div>
</template>
