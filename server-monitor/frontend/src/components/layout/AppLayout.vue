<script setup lang="ts">
import { computed } from "vue";
import { useRoute } from "vue-router";

import AppSidebar from "./AppSidebar.vue";
import AppHeader from "./AppHeader.vue";

const route = useRoute();

const pageTitle = computed(() => {
  if (route.meta.title && typeof route.meta.title === "string") {
    return route.meta.title;
  }
  return "";
});

const isFullBleed = computed(() => route.meta.fullBleed === true);
</script>

<template>
  <el-container class="app-layout">
    <AppSidebar />
    <el-container class="app-main-container">
      <AppHeader :page-title="pageTitle" />
      <el-main
        class="app-main"
        :class="{ 'app-main--full-bleed': isFullBleed }"
      >
        <RouterView />
      </el-main>
    </el-container>
  </el-container>
</template>

<style scoped>
.app-layout {
  height: 100vh;
  overflow: hidden;
}

.app-main-container {
  flex-direction: column;
  overflow: hidden;
  background: var(--cloudops-bg-primary);
}

.app-main {
  padding: 20px;
  overflow-y: auto;
  background: var(--cloudops-bg-primary);
}

.app-main--full-bleed {
  padding: 0;
  overflow: hidden;
}
</style>
