<script setup lang="ts">
withDefaults(defineProps<{
  as?: "article" | "div" | "section";
  width?: "content" | "text" | "full";
}>(), {
  as: "section",
  width: "content",
});
</script>

<template>
  <component
    :is="as"
    class="workspace-page-frame"
    :class="`workspace-page-frame--${width}`"
  >
    <slot />
  </component>
</template>

<style scoped>
.workspace-page-frame {
  width: 100%;
  min-width: 0;
  margin-inline: auto;
}

.workspace-page-frame--content { max-width: var(--co-content-max-width); }
.workspace-page-frame--text { max-width: var(--co-text-content-max-width); }
.workspace-page-frame--full { max-width: none; margin-inline: 0; }
</style>
