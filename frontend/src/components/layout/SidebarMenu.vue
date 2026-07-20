<script setup lang="ts">
import { computed, markRaw } from "vue";
import { useRoute, useRouter } from "vue-router";

import { FirstAidKit } from "@element-plus/icons-vue";

import { primaryNavigation } from "../../navigation";

defineProps<{
  collapsed: boolean;
}>();

const route = useRoute();
const router = useRouter();
const iconMap: Record<string, typeof FirstAidKit> = {
  FirstAidKit: markRaw(FirstAidKit),
};

const visibleItems = computed(() => primaryNavigation);

const activeMenu = computed(() => {
  const path = route.path;

  if (path.startsWith("/incidents/")) return "/incidents";

  return path;
});

function handleSelect(index: string) {
  if (index.includes("-group")) return;
  router.push(index);
}
</script>

<template>
  <el-menu
    :default-active="activeMenu"
    :collapse="collapsed"
    :collapse-transition="false"
    class="sidebar-menu"
    background-color="transparent"
    :text-color="'var(--cloudops-text-secondary)'"
    :active-text-color="'var(--cloudops-accent)'"
    @select="handleSelect"
  >
    <template
      v-for="item in visibleItems"
      :key="item.index"
    >
      <el-menu-item
        :index="item.index"
      >
        <el-icon v-if="item.icon && iconMap[item.icon]">
          <component :is="iconMap[item.icon]" />
        </el-icon>
        <template #title>
          {{ item.title }}
        </template>
      </el-menu-item>
    </template>
  </el-menu>
</template>

<style scoped>
.sidebar-menu {
  border-right: none;
  flex: 1;
  overflow-y: auto;
  padding-top: 8px;
}

.sidebar-menu:not(.el-menu--collapse) {
  width: 220px;
}

:deep(.el-menu-item),
:deep(.el-menu-item) {
  height: 40px;
  line-height: 40px;
  margin: 2px 8px;
  border-radius: 6px;
}

:deep(.el-menu-item.is-active) {
  background: var(--cloudops-bg-active) !important;
  color: var(--cloudops-accent) !important;
}

:deep(.el-menu-item:hover) {
  background: var(--cloudops-bg-hover) !important;
}

</style>
