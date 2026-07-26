<script setup lang="ts">
import { markRaw, type Component } from "vue";
import { Bot, Ellipsis, LayoutDashboard, Siren, Stethoscope } from "lucide-vue-next";
import { useRoute } from "vue-router";

import { mobilePrimaryNavigation, type NavigationIcon } from "../../navigation";

const emit = defineEmits<{ openMore: [trigger: HTMLElement] }>();
const route = useRoute();
const icons: Partial<Record<NavigationIcon, Component>> = {
  LayoutDashboard: markRaw(LayoutDashboard), Siren: markRaw(Siren), Bot: markRaw(Bot), FirstAid: markRaw(Stethoscope),
};
const active = (path: string) => route.path === path || route.path.startsWith(`${path}/`);

function openMore(event: MouseEvent) {
  emit("openMore", event.currentTarget as HTMLElement);
}
</script>

<template>
  <nav class="mobile-bottom-nav" aria-label="移动工作区导航">
    <RouterLink v-for="item in mobilePrimaryNavigation" :key="item.index" :to="item.index" :class="{ 'is-active': active(item.index) }" :aria-current="active(item.index) ? 'page' : undefined">
      <component :is="icons[item.icon]" :size="20" aria-hidden="true" /><span>{{ item.shortTitle }}</span>
    </RouterLink>
    <button type="button" aria-label="更多工作区" title="更多工作区" @click="openMore"><Ellipsis :size="21" aria-hidden="true" /><span>更多</span></button>
  </nav>
</template>

<style scoped>
.mobile-bottom-nav { position: fixed; right: 0; bottom: 0; left: 0; z-index: var(--co-z-header); display: none; height: calc(58px + env(safe-area-inset-bottom)); grid-template-columns: repeat(5, minmax(0, 1fr)); padding: 4px max(4px, env(safe-area-inset-right)) env(safe-area-inset-bottom) max(4px, env(safe-area-inset-left)); border-top: 1px solid var(--co-border-default); background: var(--co-bg-surface); }
.mobile-bottom-nav a, .mobile-bottom-nav button { display: grid; min-width: 0; min-height: 50px; place-content: center; justify-items: center; gap: 2px; padding: 0; border: 0; border-radius: var(--co-radius-control); color: var(--co-text-muted); background: transparent; font-size: 10px; font-weight: 700; }
.mobile-bottom-nav a:hover, .mobile-bottom-nav button:hover { color: var(--co-text-primary); background: var(--co-bg-hover); }
.mobile-bottom-nav a.is-active { color: var(--co-action-primary); background: var(--co-bg-active); }
.mobile-bottom-nav button { cursor: pointer; }
@media (max-width: 767px) { .mobile-bottom-nav { display: grid; } }
</style>
