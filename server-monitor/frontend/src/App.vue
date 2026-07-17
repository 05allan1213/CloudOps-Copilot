<script setup lang="ts">
import { computed } from "vue";
import { RouterView, useRoute } from "vue-router";

import AppLayout from "./components/layout/AppLayout.vue";

const route = useRoute();

const isPublicRoute = computed(() => Boolean(route.meta.public));
</script>

<template>
  <RouterView v-if="isPublicRoute" />
  <AppLayout v-else>
    <RouterView />
  </AppLayout>
</template>
