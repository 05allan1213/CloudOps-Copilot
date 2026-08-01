<script setup lang="ts">
import type { ButtonProps } from "@nuxt/ui";
import { computed } from "vue";

import { useCopyFeedback } from "../../composables/useCopyFeedback";

const props = withDefaults(defineProps<{
  value: string;
  label: string;
  successLabel?: string;
  failureLabel?: string;
  color?: ButtonProps["color"];
  variant?: ButtonProps["variant"];
  size?: ButtonProps["size"];
  disabled?: boolean;
}>(), {
  successLabel: "已复制完整值",
  failureLabel: "复制失败",
  color: "neutral",
  variant: "ghost",
  size: "xs",
  disabled: false,
});

const emit = defineEmits<{
  copied: [];
  failed: [];
}>();

const feedback = useCopyFeedback();
const statusLabel = computed(() => {
  if (feedback.copied.value) return props.successLabel;
  if (feedback.failed.value) return props.failureLabel;
  return "";
});

async function copy() {
  if (props.disabled) return;
  if (await feedback.copy(props.value)) emit("copied");
  else emit("failed");
}
</script>

<template>
  <span class="copy-feedback-control">
    <UTooltip :text="label">
      <UButton
        :color="color"
        :variant="variant"
        :size="size"
        :icon="feedback.copied.value ? 'i-lucide-copy-check' : 'i-lucide-copy'"
        square
        :disabled="disabled || !value"
        :aria-label="label"
        @click="copy"
      />
    </UTooltip>
    <span
      class="visually-hidden"
      role="status"
      aria-live="polite"
    >{{ statusLabel }}</span>
  </span>
</template>

<style scoped>
.copy-feedback-control { display: contents; }
</style>
