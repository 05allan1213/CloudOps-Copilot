import { getApiData } from "./client";
import type { HostGroup } from "../types";

export async function fetchHostGroups(): Promise<HostGroup[]> {
  return (await getApiData<HostGroup[]>("/api/v1/host-groups")) ?? [];
}
