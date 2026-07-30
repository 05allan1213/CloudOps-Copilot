<script setup lang="ts">
import type { TableColumn } from "@nuxt/ui";
import type { BufferGeometry, InstancedMesh, LineBasicMaterial, Material, PerspectiveCamera, Scene, WebGLRenderer } from "three";
import type { OrbitControls } from "three/examples/jsm/controls/OrbitControls.js";
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";

import { createAtlasNodes, type AtlasNodeFixture } from "../fixtures";

const nodes = createAtlasNodes(200);
const stage = ref<HTMLDivElement | null>(null);
const canvasHost = ref<HTMLDivElement | null>(null);
const canvasMount = ref<HTMLDivElement | null>(null);
const viewMode = ref<"topology" | "structured">("topology");
const renderState = ref<"loading" | "ready" | "failed" | "lost" | "disposed">("loading");
const selectedIndex = ref<number | null>(17);
const search = ref("");
const fps = ref(0);
const renderCalls = ref(0);
const dpr = ref(1);
const paused = ref(false);
const disposalVerified = ref(false);
let renderer: WebGLRenderer | null = null;
let scene: Scene | null = null;
let camera: PerspectiveCamera | null = null;
let controls: OrbitControls | null = null;
let nodeMesh: InstancedMesh | null = null;
let edgeGeometry: BufferGeometry | null = null;
let edgeMaterial: LineBasicMaterial | null = null;
let resizeObserver: ResizeObserver | null = null;
let frameCounter = 0;
let totalFrames = 0;
let lastFpsAt = 0;
let threeModule: typeof import("three") | null = null;
let colorProbe: HTMLSpanElement | null = null;

const modeItems = [
  { label: "拓扑", value: "topology", icon: "i-lucide-orbit" },
  { label: "结构化", value: "structured", icon: "i-lucide-list-tree" },
];

const selectedNode = computed(() => selectedIndex.value === null ? null : nodes[selectedIndex.value] ?? null);
const filteredNodes = computed(() => {
  const query = search.value.trim().toLocaleLowerCase();
  if (!query) return nodes;
  return nodes.filter((node) => `${node.id} ${node.name} ${node.namespace} ${node.kind} ${node.status}`.toLocaleLowerCase().includes(query));
});

const tableColumns: TableColumn<AtlasNodeFixture>[] = [
  { accessorKey: "status", header: "状态" },
  { accessorKey: "kind", header: "类型" },
  { accessorKey: "name", header: "资源" },
  { accessorKey: "namespace", header: "Namespace" },
  { accessorKey: "id", header: "ID" },
];

function token(name: string) {
  if (!colorProbe) {
    colorProbe = document.createElement("span");
    colorProbe.hidden = true;
    document.body.appendChild(colorProbe);
  }
  colorProbe.style.color = `var(${name})`;
  return getComputedStyle(colorProbe).color;
}

function statusHex(status: AtlasNodeFixture["status"]) {
  const values = {
    healthy: token("--co-success-fg"),
    warning: token("--co-warning-fg"),
    critical: token("--co-critical-fg"),
    unknown: token("--co-text-muted"),
  };
  return values[status];
}

function resizeRenderer() {
  const host = canvasHost.value;
  if (!renderer || !camera || !host) return;
  const width = Math.max(320, Math.floor(host.clientWidth));
  const height = Math.max(420, Math.floor(host.clientHeight));
  dpr.value = Math.min(window.devicePixelRatio || 1, width <= 1100 ? 1 : 1.5);
  renderer.setPixelRatio(dpr.value);
  renderer.setSize(width, height, false);
  camera.aspect = width / height;
  camera.updateProjectionMatrix();
}

function renderFrame(time: number) {
  if (!renderer || !scene || !camera || paused.value) return;
  controls?.update();
  renderer.render(scene, camera);
  frameCounter += 1;
  totalFrames += 1;
  if (time - lastFpsAt >= 1000) {
    fps.value = Math.round((frameCounter * 1000) / Math.max(1, time - lastFpsAt));
    renderCalls.value = totalFrames;
    frameCounter = 0;
    lastFpsAt = time;
  }
}

function startRendering() {
  if (!renderer || renderState.value !== "ready") return;
  paused.value = false;
  lastFpsAt = performance.now();
  frameCounter = 0;
  renderer.setAnimationLoop(renderFrame);
}

function stopRendering() {
  paused.value = true;
  renderer?.setAnimationLoop(null);
}

function updateTheme() {
  if (!threeModule || !renderer || !scene || !nodeMesh) return;
  scene.background = new threeModule.Color(token("--co-canvas"));
  for (let index = 0; index < nodes.length; index += 1) nodeMesh.setColorAt(index, new threeModule.Color(statusHex(nodes[index].status)));
  if (nodeMesh.instanceColor) nodeMesh.instanceColor.needsUpdate = true;
  if (edgeMaterial) edgeMaterial.color.set(token("--co-border-strong"));
}

function handleVisibility() {
  if (document.hidden) stopRendering();
  else if (renderState.value === "ready" && viewMode.value === "topology") startRendering();
}

function handleContextLost(event: Event) {
  event.preventDefault();
  renderState.value = "lost";
  stopRendering();
  viewMode.value = "structured";
}

function handleContextRestored() {
  renderState.value = "ready";
  if (viewMode.value === "topology") startRendering();
}

function pickNode(event: PointerEvent) {
  if (!threeModule || !renderer || !camera || !nodeMesh) return;
  const rect = renderer.domElement.getBoundingClientRect();
  const pointer = new threeModule.Vector2(
    ((event.clientX - rect.left) / rect.width) * 2 - 1,
    -((event.clientY - rect.top) / rect.height) * 2 + 1,
  );
  const raycaster = new threeModule.Raycaster();
  raycaster.setFromCamera(pointer, camera);
  const hit = raycaster.intersectObject(nodeMesh, false)[0];
  if (typeof hit?.instanceId === "number") selectedIndex.value = hit.instanceId;
}

async function initializeAtlas() {
  const host = canvasMount.value;
  if (!host || viewMode.value !== "topology") return;
  if (new URLSearchParams(window.location.search).get("webgl") === "fail") {
    renderState.value = "failed";
    viewMode.value = "structured";
    return;
  }

  try {
    threeModule = await import("three");
    const { OrbitControls: OrbitControlsConstructor } = await import("three/examples/jsm/controls/OrbitControls.js");
    if (!canvasMount.value || viewMode.value !== "topology") return;
    const THREE = threeModule;
    scene = new THREE.Scene();
    camera = new THREE.PerspectiveCamera(48, 1, 0.1, 120);
    camera.position.set(0, 8.5, 19);

    renderer = new THREE.WebGLRenderer({ antialias: true, alpha: false, preserveDrawingBuffer: false, powerPreference: "high-performance", failIfMajorPerformanceCaveat: false });
    renderer.outputColorSpace = THREE.SRGBColorSpace;
    renderer.domElement.dataset.testid = "atlas-canvas";
    renderer.domElement.setAttribute("aria-hidden", "true");
    renderer.domElement.addEventListener("pointerdown", pickNode);
    renderer.domElement.addEventListener("webglcontextlost", handleContextLost);
    renderer.domElement.addEventListener("webglcontextrestored", handleContextRestored);
    host.replaceChildren(renderer.domElement);

    controls = new OrbitControlsConstructor(camera, renderer.domElement);
    controls.enableDamping = true;
    controls.dampingFactor = 0.08;
    controls.minDistance = 8;
    controls.maxDistance = 34;
    controls.maxPolarAngle = Math.PI * 0.82;

    scene.add(new THREE.AmbientLight(0xffffff, 1.5));
    const directional = new THREE.DirectionalLight(0xffffff, 2.2);
    directional.position.set(8, 12, 10);
    scene.add(directional);

    const nodeGeometry = new THREE.SphereGeometry(0.18, 14, 10);
    const nodeMaterial = new THREE.MeshStandardMaterial({ roughness: 0.58, metalness: 0.08, vertexColors: true });
    nodeMesh = new THREE.InstancedMesh(nodeGeometry, nodeMaterial, nodes.length);
    const matrix = new THREE.Matrix4();
    nodes.forEach((node, index) => {
      matrix.makeTranslation(node.x, node.y, node.z);
      nodeMesh?.setMatrixAt(index, matrix);
      nodeMesh?.setColorAt(index, new THREE.Color(statusHex(node.status)));
    });
    nodeMesh.instanceMatrix.needsUpdate = true;
    scene.add(nodeMesh);

    const edgePositions: number[] = [];
    nodes.forEach((node) => {
      if (node.parentIndex === null) return;
      const parent = nodes[node.parentIndex];
      edgePositions.push(node.x, node.y, node.z, parent.x, parent.y, parent.z);
    });
    edgeGeometry = new THREE.BufferGeometry();
    edgeGeometry.setAttribute("position", new THREE.Float32BufferAttribute(edgePositions, 3));
    edgeMaterial = new THREE.LineBasicMaterial({ color: token("--co-border-strong"), transparent: true, opacity: 0.55 });
    scene.add(new THREE.LineSegments(edgeGeometry, edgeMaterial));

    const grid = new THREE.GridHelper(24, 24, token("--co-border-strong"), token("--co-border"));
    grid.position.y = -3.2;
    scene.add(grid);

    resizeRenderer();
    renderState.value = "ready";
    updateTheme();
    startRendering();
  } catch (error) {
    console.error("Atlas WebGL initialization failed", error);
    renderState.value = "failed";
    viewMode.value = "structured";
  }
}

function disposeAtlas() {
  stopRendering();
  const canvas = renderer?.domElement;
  canvas?.removeEventListener("pointerdown", pickNode);
  canvas?.removeEventListener("webglcontextlost", handleContextLost);
  canvas?.removeEventListener("webglcontextrestored", handleContextRestored);
  controls?.dispose();
  controls = null;
  if (scene) {
    scene.traverse((object) => {
      const mesh = object as typeof object & { geometry?: BufferGeometry; material?: Material | Material[] };
      mesh.geometry?.dispose();
      if (Array.isArray(mesh.material)) mesh.material.forEach((material) => material.dispose());
      else mesh.material?.dispose();
    });
  }
  edgeGeometry?.dispose();
  edgeMaterial?.dispose();
  renderer?.forceContextLoss();
  renderer?.dispose();
  canvas?.remove();
  renderer = null;
  scene = null;
  camera = null;
  nodeMesh = null;
  edgeGeometry = null;
  edgeMaterial = null;
  colorProbe?.remove();
  colorProbe = null;
  disposalVerified.value = true;
}

function simulateContextLoss() {
  renderer?.forceContextLoss();
}

function disposeAndFallback() {
  disposeAtlas();
  renderState.value = "disposed";
  viewMode.value = "structured";
}

function selectNode(index: number) {
  selectedIndex.value = index;
  void nextTick(resizeRenderer);
}

function closeInspector() {
  selectedIndex.value = null;
  void nextTick(resizeRenderer);
}

watch(viewMode, async (value) => {
  await nextTick();
  if (value === "topology") {
    if (!renderer) {
      renderState.value = "loading";
      await initializeAtlas();
    } else startRendering();
  } else stopRendering();
});
watch(selectedIndex, () => void nextTick(resizeRenderer));

onMounted(() => {
  resizeObserver = new ResizeObserver(resizeRenderer);
  if (stage.value) resizeObserver.observe(stage.value);
  window.addEventListener("cloudops-prework-theme", updateTheme);
  document.addEventListener("visibilitychange", handleVisibility);
  void initializeAtlas();
});

onBeforeUnmount(() => {
  resizeObserver?.disconnect();
  resizeObserver = null;
  window.removeEventListener("cloudops-prework-theme", updateTheme);
  document.removeEventListener("visibilitychange", handleVisibility);
  disposeAtlas();
});
</script>

<template>
  <section class="workspace atlas-lab" aria-labelledby="atlas-title">
    <header class="workspace-header">
      <div class="workspace-title">
        <h1 id="atlas-title" tabindex="-1">Operations Atlas</h1>
        <p>200 个 Provider-backed 资源 · WebGL 与完整结构化等价路径</p>
      </div>
      <div class="workspace-actions">
        <UBadge color="neutral" variant="subtle" icon="i-lucide-boxes" label="200 nodes" />
        <UBadge :color="paused ? 'warning' : 'success'" variant="subtle" :icon="paused ? 'i-lucide-pause' : 'i-lucide-gauge'" :label="paused ? 'hidden/paused' : `${fps} FPS · DPR ${dpr}`" data-testid="atlas-fps" />
      </div>
    </header>

    <section class="toolbar-band atlas-toolbar" aria-label="Atlas 工具栏">
      <UTabs v-model="viewMode" :items="modeItems" value-key="value" size="sm" data-testid="atlas-mode" />
      <UButton color="neutral" variant="outline" icon="i-lucide-crosshair" label="定位关键节点" @click="selectNode(17)" />
      <UButton color="warning" variant="outline" icon="i-lucide-unplug" label="模拟 context loss" :disabled="renderState !== 'ready'" data-testid="atlas-context-loss" @click="simulateContextLoss" />
      <UButton color="neutral" variant="ghost" icon="i-lucide-trash-2" label="释放 GPU" :disabled="!renderer" data-testid="atlas-dispose" @click="disposeAndFallback" />
      <span class="atlas-state">{{ renderState }} · {{ renderCalls.toLocaleString() }} frames</span>
    </section>

    <UAlert v-if="renderState === 'failed'" color="error" variant="soft" icon="i-lucide-monitor-x" title="WebGL 创建失败" description="自动切换到 200 项结构化路径，未用装饰图替代拓扑事实。" data-testid="atlas-webgl-failed" />
    <UAlert v-else-if="renderState === 'lost'" color="warning" variant="soft" icon="i-lucide-unplug" title="WebGL context lost" description="渲染已暂停并切换结构化路径；当前选择与 Scope 保留。" data-testid="atlas-context-lost" />
    <UAlert v-else-if="renderState === 'disposed'" color="neutral" variant="soft" icon="i-lucide-circle-check" title="GPU 资源已释放" :description="disposalVerified ? 'Geometry、Material、Controls 与 Renderer dispose 已执行。' : '等待清理。'" data-testid="atlas-disposed" />

    <section v-show="viewMode === 'topology'" ref="stage" class="atlas-stage" :class="{ 'has-inspector': selectedNode }" aria-label="Atlas 3D 拓扑">
      <div ref="canvasHost" class="atlas-canvas-host" data-testid="atlas-canvas-host">
        <div ref="canvasMount" class="atlas-canvas-mount" />
        <div v-if="renderState === 'loading'" class="atlas-loading"><UIcon name="i-lucide-loader-circle" class="spinning" aria-hidden="true" /><span>初始化 200 节点拓扑</span></div>
        <div class="atlas-legend" aria-hidden="true">
          <span><i class="healthy" />正常</span><span><i class="warning" />告警</span><span><i class="critical" />严重</span><span><i class="unknown" />未知</span>
        </div>
      </div>
      <aside v-if="selectedNode" class="atlas-inspector" aria-labelledby="atlas-inspector-title" data-testid="atlas-inspector">
        <div class="atlas-inspector-heading">
          <div><span>{{ selectedNode.kind }}</span><h2 id="atlas-inspector-title">{{ selectedNode.name }}</h2></div>
          <UTooltip text="关闭 Inspector"><UButton color="neutral" variant="ghost" square icon="i-lucide-x" aria-label="关闭 Atlas Inspector" @click="closeInspector" /></UTooltip>
        </div>
        <UBadge :color="selectedNode.status === 'critical' ? 'error' : selectedNode.status === 'warning' ? 'warning' : selectedNode.status === 'healthy' ? 'success' : 'neutral'" :icon="selectedNode.status === 'healthy' ? 'i-lucide-circle-check' : 'i-lucide-circle-alert'" :label="selectedNode.status" />
        <dl>
          <div class="data-pair"><dt>ID</dt><dd class="mono">{{ selectedNode.id }}</dd></div>
          <div class="data-pair"><dt>Namespace</dt><dd class="mono">{{ selectedNode.namespace }}</dd></div>
          <div class="data-pair"><dt>Provider</dt><dd>Kubernetes typed reader</dd></div>
          <div class="data-pair"><dt>Observed UTC</dt><dd class="mono">2026-07-30T08:42:19Z</dd></div>
        </dl>
        <UButton color="primary" variant="outline" icon="i-lucide-list-tree" label="在结构化路径定位" @click="viewMode = 'structured'; search = selectedNode.id" />
      </aside>
    </section>

    <section v-if="viewMode === 'structured'" class="structured-atlas" aria-labelledby="structured-title" data-testid="atlas-structured">
      <div class="section-heading"><div><h2 id="structured-title">结构化拓扑</h2><span>{{ filteredNodes.length }} / 200 资源</span></div><UInput v-model="search" icon="i-lucide-search" placeholder="资源、ID、Namespace" data-testid="atlas-search" /></div>
      <UTable :data="filteredNodes" :columns="tableColumns" sticky class="atlas-table" data-testid="atlas-table" />
    </section>
  </section>
</template>

<style scoped>
.atlas-lab { max-width: none; padding-inline: 0; }
.atlas-lab > .workspace-header, .atlas-lab > .toolbar-band, .atlas-lab > [role="alert"] { margin-inline: var(--co-space-5); }
.atlas-toolbar { align-items: center; }
.atlas-state { margin-left: auto; color: var(--co-text-muted); font-family: var(--co-font-mono); font-size: 10px; }
.atlas-stage { display: grid; min-width: 0; height: max(540px, calc(100dvh - 230px)); grid-template-columns: minmax(0, 1fr); border-block: 1px solid var(--co-border); background: var(--co-canvas); transition: grid-template-columns var(--co-motion-standard) var(--co-ease); }
.atlas-stage.has-inspector { grid-template-columns: minmax(0, 1fr) minmax(300px, 32%); }
.atlas-canvas-host { position: relative; min-width: 0; min-height: 420px; overflow: hidden; }
.atlas-canvas-mount { position: absolute; inset: 0; }
.atlas-canvas-mount :deep(canvas) { display: block; width: 100%; height: 100%; }
.atlas-loading { position: absolute; inset: 0; display: grid; place-content: center; justify-items: center; gap: 8px; color: var(--co-text-muted); font-size: 11px; }
.spinning { animation: spin 0.8s linear infinite; }
.atlas-legend { position: absolute; bottom: 14px; left: 14px; display: flex; flex-wrap: wrap; gap: 10px; padding: 7px 9px; border: 1px solid var(--co-border); border-radius: var(--co-radius-control); color: var(--co-text-secondary); background: color-mix(in srgb, var(--co-overlay) 92%, transparent); font-size: 10px; }
.atlas-legend span { display: inline-flex; align-items: center; gap: 5px; }
.atlas-legend i { width: 7px; height: 7px; border-radius: 50%; background: var(--co-text-muted); }
.atlas-legend .healthy { background: var(--co-success-fg); }.atlas-legend .warning { background: var(--co-warning-fg); }.atlas-legend .critical { background: var(--co-critical-fg); }.atlas-legend .unknown { background: var(--co-text-muted); }
.atlas-inspector { min-width: 0; padding: var(--co-space-4); overflow: auto; border-left: 1px solid var(--co-border); background: var(--co-surface); }
.atlas-inspector-heading { display: flex; min-width: 0; align-items: flex-start; justify-content: space-between; gap: var(--co-space-2); margin-bottom: var(--co-space-3); }
.atlas-inspector-heading span { color: var(--co-text-muted); font-size: 10px; text-transform: uppercase; }
.atlas-inspector-heading h2 { margin: 2px 0 0; overflow-wrap: anywhere; }
.structured-atlas { min-width: 0; margin: var(--co-space-4) var(--co-space-5); border-block: 1px solid var(--co-border); background: var(--co-surface); }
.structured-atlas > .section-heading { padding: var(--co-space-3); }
.structured-atlas .section-heading span { color: var(--co-text-muted); font-size: 10px; }
.atlas-table { max-height: max(440px, calc(100dvh - 300px)); overflow: auto; }
.atlas-table :deep(th), .atlas-table :deep(td) { height: 34px; padding-block: 4px; font-size: 10px; }
@keyframes spin { to { transform: rotate(360deg); } }
@media (max-width: 1100px) {
  .atlas-lab > .workspace-header, .atlas-lab > .toolbar-band, .atlas-lab > [role="alert"] { margin-inline: var(--co-space-4); }
  .atlas-stage.has-inspector { grid-template-columns: minmax(0, 1fr) minmax(280px, 38%); }
}
</style>
