<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";

export interface IncidentZone {
  id: string;
  label: string;
  index: string;
}

const props = defineProps<{
  zones: IncidentZone[];
}>();

const route = useRoute();
const router = useRouter();
const activeZone = ref(zoneFromHash(route.hash) || props.zones[0]?.id || "");
let observer: IntersectionObserver | null = null;

function zoneFromHash(hash: string): string {
  const id = hash.replace(/^#/, "");
  return props.zones.some((zone) => zone.id === id) ? id : "";
}

function navigateToZone(zoneID: string) {
  if (!props.zones.some((zone) => zone.id === zoneID)) return;
  activeZone.value = zoneID;
  document.getElementById(zoneID)?.scrollIntoView({ block: "start" });
  if (route.hash !== `#${zoneID}`) void router.replace({ hash: `#${zoneID}` });
}

function onZoneSelect(event: Event) {
  navigateToZone((event.target as HTMLSelectElement).value);
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
  const scrollRoot = document.querySelector<HTMLElement>(".app-main");
  if (typeof IntersectionObserver === "function") {
    observer = new IntersectionObserver((entries) => {
      const visible = entries
        .filter((entry) => entry.isIntersecting)
        .sort((left, right) => Math.abs(left.boundingClientRect.top) - Math.abs(right.boundingClientRect.top));
      const next = visible[0]?.target.id;
      if (!next || next === activeZone.value) return;
      activeZone.value = next;
      if (route.hash !== `#${next}`) void router.replace({ hash: `#${next}` });
    }, {
      root: scrollRoot,
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
          @click.prevent="navigateToZone(zone.id)"
        >
          <span aria-hidden="true">{{ zone.index }}</span>
          {{ zone.label }}
        </a>
      </li>
    </ol>

    <div class="mobile-zone-select">
      <label for="incident-zone-select">Jump to section</label>
      <select
        id="incident-zone-select"
        name="incident_zone"
        autocomplete="off"
        :value="activeZone"
        @change="onZoneSelect"
      >
        <option
          v-for="zone in zones"
          :key="zone.id"
          :value="zone.id"
        >
          {{ zone.index }} · {{ zone.label }}
        </option>
      </select>
    </div>
  </nav>
</template>

<style scoped>
.zone-nav {
  position: sticky;
  top: 0;
  z-index: var(--co-z-sticky);
  min-width: 0;
  border-block: 1px solid var(--co-border-default);
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
  color: var(--co-text-muted);
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
    color: var(--co-text-muted);
    font-size: 11px;
    font-weight: 700;
    text-transform: uppercase;
  }

  .mobile-zone-select select {
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
