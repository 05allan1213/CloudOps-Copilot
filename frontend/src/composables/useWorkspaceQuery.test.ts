import { describe, expect, it } from "vitest";

import {
  createWorkspaceQueryCodec,
  enumQueryField,
  integerQueryField,
  stringQueryField,
} from "./useWorkspaceQuery";

interface QueryState {
  search: string;
  sort: "updated-desc" | "updated-asc";
  page: number;
  tab: "current" | "history";
  selected: string;
}

const codec = createWorkspaceQueryCodec<QueryState>({
  search: stringQueryField("search"),
  sort: enumQueryField("sort", ["updated-desc", "updated-asc"], "updated-desc", ["order"]),
  page: integerQueryField("page", { defaultValue: 1, min: 1, max: 500 }),
  tab: enumQueryField("tab", ["current", "history"], "current"),
  selected: stringQueryField("selected", { aliases: ["incident"] }),
}, { transientKeys: ["hover", "menu", "columns"] });

describe("Workspace URL codec", () => {
  it("accepts legacy aliases and rejects invalid controlled values", () => {
    expect(codec.decode({
      search: "payments",
      order: "updated-asc",
      page: "not-a-page",
      tab: "unknown",
      incident: "inc-42",
    })).toEqual({
      search: "payments",
      sort: "updated-asc",
      page: 1,
      tab: "current",
      selected: "inc-42",
    });
  });

  it("emits canonical URL state without local UI preferences", () => {
    expect(codec.encode({
      search: "payments",
      sort: "updated-asc",
      page: 3,
      tab: "history",
      selected: "inc-42",
    }, {
      cluster: "local",
      incident: "legacy",
      order: "legacy",
      hover: "row-1",
      menu: "open",
      columns: "owner,status",
    })).toEqual({
      cluster: "local",
      search: "payments",
      sort: "updated-asc",
      page: "3",
      tab: "history",
      selected: "inc-42",
    });
  });

  it("omits default values for stable shareable URLs", () => {
    expect(codec.encode({
      search: "",
      sort: "updated-desc",
      page: 1,
      tab: "current",
      selected: "",
    })).toEqual({});
  });
});
