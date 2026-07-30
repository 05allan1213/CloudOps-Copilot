<script setup lang="ts">
const state = { name: "CloudOps" };
const rows = [{ name: "api", status: "available" }];
</script>

<template>
  <UApp>
    <UButton
      icon="i-lucide-check"
      label="确认"
    />
    <UForm :state="state">
      <UFormField
        label="名称"
        name="name"
      >
        <UInput v-model="state.name" />
      </UFormField>
    </UForm>
    <UTable :data="rows" />
    <UModal
      :open="false"
      title="确认操作"
    />
    <USlideover
      :open="false"
      title="资源详情"
    />
  </UApp>
</template>
