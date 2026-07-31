<script setup lang="ts">
import { computed } from "vue";

import { apiErrorDetails } from "../../api/client";
import WorkspaceState from "./WorkspaceState.vue";

const props = withDefaults(defineProps<{
  error: unknown;
  fallback?: string;
  title?: string;
  retryable?: boolean;
}>(), {
  fallback: "请求失败，请检查当前 API 与 Provider 状态。",
  title: "请求失败",
  retryable: false,
});

const emit = defineEmits<{
  retry: [];
}>();

const details = computed(() => apiErrorDetails(props.error, props.fallback));
</script>

<template>
  <WorkspaceState
    kind="error"
    :title="title"
    :description="details.message"
    :code="details.code"
    :request-i-d="details.requestID"
    :trace-i-d="details.traceID"
    :idempotent-replay="details.idempotentReplay"
    :next-steps="details.nextSteps"
  >
    <template
      v-if="retryable"
      #actions
    >
      <UButton
        color="error"
        variant="soft"
        icon="i-lucide-rotate-cw"
        label="重试"
        @click="emit('retry')"
      />
    </template>
  </WorkspaceState>
</template>
