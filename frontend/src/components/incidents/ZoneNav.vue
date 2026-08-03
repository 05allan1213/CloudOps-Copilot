<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";

export interface IncidentZone {
  id: string;
  label: string;
  index: string;
  aliases?: string[];
}

const props = defineProps<{
  zones: IncidentZone[];
}>();

const route = useRoute();
const router = useRouter();
const activeZone = ref(zoneFromHash(route.hash) || props.zones[0]?.id || "");
let observer: IntersectionObserver | null = null;
const zoneItems = computed(() => props.zones.map((zone) => ({
  label: `${zone.index} · ${zone.label}`,
  value: zone.id,
})));

function zoneFromHash(hash: string): string {
  const id = hash.replace(/^#/, "");
  return props.zones.find((zone) => zone.id === id || zone.aliases?.includes(id))?.id ?? "";
}

function navigateToZone(zoneID: string) {
  if (!props.zones.some((zone) => zone.id === zoneID)) return;
  activeZone.value = zoneID;
  document.getElementById(zoneID)?.scrollIntoView({ block: "start" });
  syncZoneHash(zoneID);
}

function onDesktopZoneClick(event: MouseEvent, zoneID: string) {
  // Keep native anchor behavior for Cmd/Ctrl-click, middle-click, and other
  // modified clicks so users can open a deep-linked zone in another tab.
  if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
  event.preventDefault();
  navigateToZone(zoneID);
}

function syncZoneHash(zoneID: string) {
  const nextHash = `#${zoneID}`;
  if (route.hash === nextHash) return;
  // The user action or document scroll remains the scroll owner. The router
  // synchronizes this shareable location without initiating another scroll.
  void router.replace({
    path: route.path,
    query: route.query,
    hash: nextHash,
  }).catch(() => {
    // The visible scroll and active state remain useful if a navigation guard
    // rejects a cosmetic hash update.
  });
}

function onZoneSelect(value: string) {
  navigateToZone(value);
}

watch(
  () => route.hash,
  (hash) => {
    const zone = zoneFromHash(hash);
    if (zone) activeZone.value = zone;
  },
);

onMounted(async () => {
  await nextTick();
  if (typeof IntersectionObserver === "function") {
    observer = new IntersectionObserver((entries) => {
      const visible = entries
        .filter((entry) => entry.isIntersecting)
        .sort((left, right) => Math.abs(left.boundingClientRect.top) - Math.abs(right.boundingClientRect.top));
      const next = visible[0]?.target.id;
      if (!next || next === activeZone.value) return;
      activeZone.value = next;
      syncZoneHash(next);
    }, {
      root: null,
      rootMargin: "-12px 0px -68% 0px",
      threshold: [0, 0.01, 0.2],
    });
    for (const zone of props.zones) {
      const element = document.getElementById(zone.id);
      if (element) observer.observe(element);
    }
  }
  const deepLink = zoneFromHash(route.hash);
  if (deepLink) window.requestAnimationFrame(() => navigateToZone(deepLink));
});

onBeforeUnmount(() => observer?.disconnect());
</script>

<template>
  <nav
    class="zone-nav"
    aria-label="Incident detail zones"
  >
    <ol class="desktop-zone-list">
      <li
        v-for="zone in zones"
        :key="zone.id"
      >
        <a
          :href="`#${zone.id}`"
          :aria-current="activeZone === zone.id ? 'location' : undefined"
          @click="onDesktopZoneClick($event, zone.id)"
        >
          <span aria-hidden="true">{{ zone.index }}</span>
          {{ zone.label }}
        </a>
      </li>
    </ol>

    <div class="mobile-zone-select">
      <label for="incident-zone-select">跳转到生命周期区块</label>
      <USelect
        id="incident-zone-select"
        :model-value="activeZone"
        :items="zoneItems"
        value-key="value"
        aria-label="跳转到生命周期区块"
        @update:model-value="onZoneSelect"
      />
    </div>
  </nav>
</template>

<style scoped>
.zone-nav {
  position: sticky;
  top: 0;
  z-index: var(--co-z-sticky);
  min-width: 0;
  overflow: hidden;
  border: 1px solid var(--co-border-default);
  border-radius: var(--co-radius-frame);
  background: color-mix(in srgb, var(--co-bg-canvas) 94%, transparent);
  backdrop-filter: blur(10px);
}

.desktop-zone-list {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  margin: 0;
  padding: 0;
  list-style: none;
}

.desktop-zone-list li + li { border-left: 1px solid var(--co-border-default); }

.desktop-zone-list a {
  display: flex;
  min-height: 48px;
  align-items: center;
  gap: var(--co-space-2);
  padding: 0 var(--co-space-4);
  color: var(--co-text-secondary);
  font-size: 13px;
  font-weight: 650;
}

.desktop-zone-list a span {
  color: var(--co-text-secondary);
  font-family: var(--co-font-mono);
  font-size: 11px;
}

.desktop-zone-list a:hover {
  color: var(--co-text-primary);
  background: var(--co-bg-hover);
}

.desktop-zone-list a[aria-current="location"] {
  color: var(--co-action-primary);
  background: var(--co-bg-active);
  box-shadow: inset 0 -2px 0 var(--co-action-primary);
}

.mobile-zone-select { display: none; }

@media (max-width: 767px) {
  .desktop-zone-list { display: none; }

  .mobile-zone-select {
    display: grid;
    gap: var(--co-space-1);
    padding: var(--co-space-2) 0;
  }

  .mobile-zone-select label {
    color: var(--co-text-secondary);
    font-size: 11px;
    font-weight: 700;
    text-transform: uppercase;
  }

  .mobile-zone-select :deep(button) {
    width: 100%;
    min-height: 44px;
    padding: 0 36px 0 var(--co-space-3);
    border: 1px solid var(--co-border-default);
    border-radius: var(--co-radius-control);
    color: var(--co-text-primary);
    background-color: var(--co-bg-surface);
    font-size: 16px;
  }
}
</style>
