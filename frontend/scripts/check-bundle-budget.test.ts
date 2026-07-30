import { describe, expect, it } from "vitest";
import {
  classifySpecialistChunks,
  collectStaticChunkFiles,
  evaluateBudget,
} from "./check-bundle-budget.mjs";

describe("bundle budget manifest helpers", () => {
  it("collects only the entry and its static imports", () => {
    const manifest = {
      "index.html": {
        file: "assets/index.js",
        imports: ["_shared.js"],
        dynamicImports: ["src/views/atlas/AtlasView.vue"],
        isEntry: true,
      },
      "_shared.js": { file: "assets/shared.js" },
      "src/views/atlas/AtlasView.vue": { file: "assets/atlas.js" },
    };

    expect(collectStaticChunkFiles(manifest, "index.html")).toEqual([
      "assets/index.js",
      "assets/shared.js",
    ]);
  });

  it("classifies emitted specialist chunks without counting route names", () => {
    const manifest = {
      "_three.module.js": { file: "assets/three.module.js", name: "three.module" },
      "_uPlot.js": { file: "assets/uPlot.js", name: "uPlot" },
      "_virtual-core.js": { file: "assets/virtual-core.js" },
      "src/views/atlas/AtlasView.vue": { file: "assets/AtlasView.js" },
    };

    expect(classifySpecialistChunks(manifest)).toEqual({
      monitoring: ["assets/uPlot.js"],
      three: ["assets/three.module.js"],
      virtualization: ["assets/virtual-core.js"],
    });
  });

  it("fails bytes above a fixed limit without rounding", () => {
    expect(evaluateBudget("main", 307200, 307200).status).toBe("PASS");
    expect(evaluateBudget("main", 307201, 307200).status).toBe("FAIL");
  });
});
