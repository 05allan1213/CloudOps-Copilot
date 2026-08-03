import type { SettingsSectionKey } from "./settingsDraft";

export type SettingsViewSection = SettingsSectionKey | "revisions";

const settingsSectionAliases: Readonly<Record<string, SettingsViewSection>> = {
  system: "system",
  "operational-scope": "scopes",
  "escalation-policies": "policies",
  providers: "providers",
  "secret-references": "secret-references",
  "revision-history": "revisions",
};

export interface SettingsSearchEntry {
  key: SettingsViewSection;
  label: string;
  text: string;
  field?: string;
}

export function resolveSettingsViewSection(hash: string): SettingsViewSection {
  return settingsSectionAliases[hash.replace(/^#/, "")] ?? "system";
}

export function filterSettingsSearchEntries(
  query: string,
  entries: readonly SettingsSearchEntry[],
): SettingsSearchEntry[] {
  const normalized = query.trim().toLowerCase();
  if (!normalized) return [];
  return entries.filter((entry) => `${entry.label} ${entry.text}`.toLowerCase().includes(normalized));
}

export function shouldBlockSettingsLeave(hasUnsavedChanges: boolean): boolean {
  return hasUnsavedChanges;
}
