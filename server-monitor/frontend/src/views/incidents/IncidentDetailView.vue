<script setup lang="ts">
import { onMounted } from "vue";
import { useRoute } from "vue-router";

import IncidentAgentRuns from "../../components/incidents/IncidentAgentRuns.vue";
import IncidentDelivery from "../../components/incidents/IncidentDelivery.vue";
import IncidentEvidence from "../../components/incidents/IncidentEvidence.vue";
import IncidentHeader from "../../components/incidents/IncidentHeader.vue";
import IncidentOverview from "../../components/incidents/IncidentOverview.vue";
import IncidentPostmortem from "../../components/incidents/IncidentPostmortem.vue";
import IncidentRemediation from "../../components/incidents/IncidentRemediation.vue";
import IncidentResources from "../../components/incidents/IncidentResources.vue";
import IncidentSignals from "../../components/incidents/IncidentSignals.vue";
import IncidentTimeline from "../../components/incidents/IncidentTimeline.vue";
import IncidentVerification from "../../components/incidents/IncidentVerification.vue";
import { useIncidentDetail } from "../../composables/incidents/useIncidentDetail";
import { useIncidentRealtime } from "../../composables/incidents/useIncidentRealtime";
import { useAuthStore } from "../../stores/auth";

const route = useRoute();
const auth = useAuthStore();
const incidentID = String(route.params.incidentId ?? "");
const detail = useIncidentDetail(incidentID, auth.isAdmin);
const realtime = useIncidentRealtime(incidentID, detail.load);

onMounted(async () => { await detail.load(); if (detail.pageState.value === "ready") realtime.start(); });
</script>

<template>
  <main class="incident-detail-view">
    <router-link
      :to="{ name: 'incidents' }"
      class="back-link"
    >
      ← All incidents
    </router-link>
    <div
      v-if="detail.pageState.value === 'loading'"
      role="status"
      aria-live="polite"
      class="page-message"
    >
      Loading incident…
    </div>
    <div
      v-else-if="detail.pageState.value === 'forbidden'"
      role="alert"
      class="page-message"
    >
      Permission denied.
    </div>
    <div
      v-else-if="detail.pageState.value === 'not_found'"
      role="status"
      class="page-message"
    >
      Incident not found.
    </div>
    <div
      v-else-if="detail.pageState.value !== 'ready' || !detail.incident.value"
      role="alert"
      class="page-message"
    >
      {{ detail.pageError.value || "Incident unavailable" }} <el-button @click="detail.load">
        Retry
      </el-button>
    </div>
    <template v-else>
      <IncidentHeader
        :incident="detail.incident.value"
        :realtime-state="realtime.state.value"
      />
      <nav
        aria-label="Incident sections"
        class="section-nav"
      >
        <a
          v-for="item in ['overview','signals','timeline','evidence','investigation','remediation','delivery','verification','postmortem','resources']"
          :key="item"
          :href="`#${item}`"
        >{{ item }}</a>
      </nav>
      <div class="section-stack">
        <IncidentOverview :incident="detail.incident.value" />
        <IncidentSignals
          :state="detail.signals.state"
          :error="detail.signals.error"
          :signals="detail.signals.data"
        />
        <IncidentTimeline
          :state="detail.timeline.state"
          :error="detail.timeline.error"
          :items="detail.timeline.data"
          :total="detail.timelineTotal.value"
          @load-more="detail.loadMoreTimeline"
        />
        <IncidentEvidence
          :state="detail.evidence.state"
          :error="detail.evidence.error"
          :items="detail.evidence.data"
        />
        <IncidentAgentRuns
          :state="detail.investigation.state"
          :error="detail.investigation.error"
          :investigation="detail.investigation.data"
        />
        <IncidentRemediation
          :state="detail.remediation.state"
          :error="detail.remediation.error"
          :remediation="detail.remediation.data"
        />
        <IncidentDelivery
          :state="detail.delivery.state"
          :error="detail.delivery.error"
          :delivery="detail.delivery.data"
        />
        <IncidentVerification
          :state="detail.verification.state"
          :error="detail.verification.error"
          :detail="detail.verification.data"
          :runs="detail.verificationRuns.data"
        />
        <IncidentPostmortem
          :state="detail.postmortem.state"
          :error="detail.postmortem.error"
          :postmortem="detail.postmortem.data"
        />
        <IncidentResources
          :state="detail.resources.state"
          :error="detail.resources.error"
          :cluster="detail.incident.value.cluster"
          :resources="detail.resources.data"
        />
      </div>
    </template>
  </main>
</template>

<style scoped>
.incident-detail-view { display: grid; gap: 16px; }.back-link { width: fit-content; color: var(--el-color-primary); }.page-message { padding: 50px; text-align: center; }.section-nav { position: sticky; top: 0; z-index: 5; display: flex; gap: 6px; overflow-x: auto; padding: 10px; border: 1px solid var(--cloudops-border-color); border-radius: 9px; background: var(--cloudops-bg-card); }.section-nav a { padding: 6px 9px; color: var(--el-text-color-regular); text-decoration: none; text-transform: capitalize; border-radius: 5px; white-space: nowrap; }.section-nav a:hover, .section-nav a:focus-visible { background: var(--el-fill-color-light); outline: 2px solid var(--el-color-primary); }.section-stack { display: grid; gap: 16px; }
</style>
