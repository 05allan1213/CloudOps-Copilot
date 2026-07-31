import type { LocationQuery, LocationQueryRaw } from "vue-router";

export interface WorkspaceQueryField<T> {
  key: string;
  aliases?: readonly string[];
  decode: (value: string | undefined) => T;
  encode: (value: T) => string | undefined;
}

export type WorkspaceQuerySchema<T extends object> = {
  [K in keyof T]: WorkspaceQueryField<T[K]>;
};

export interface WorkspaceQueryCodecOptions {
  transientKeys?: readonly string[];
}

export function firstQueryValue(value: LocationQuery[string]): string | undefined {
  if (Array.isArray(value)) return value.find((item): item is string => typeof item === "string");
  return typeof value === "string" ? value : undefined;
}

export function createWorkspaceQueryCodec<T extends object>(
  schema: WorkspaceQuerySchema<T>,
  options: WorkspaceQueryCodecOptions = {},
) {
  const entries = Object.entries(schema) as [keyof T, WorkspaceQueryField<T[keyof T]>][];

  function decode(query: LocationQuery): T {
    const result: Partial<Record<keyof T, T[keyof T]>> = {};
    for (const [stateKey, field] of entries) {
      const keys = [field.key, ...(field.aliases ?? [])];
      const value = keys.map((key) => firstQueryValue(query[key])).find((candidate) => candidate !== undefined);
      result[stateKey] = field.decode(value);
    }
    return result as T;
  }

  function encode(state: T, current: LocationQueryRaw = {}): LocationQueryRaw {
    const next = { ...current };
    for (const transientKey of options.transientKeys ?? []) delete next[transientKey];
    for (const [stateKey, field] of entries) {
      delete next[field.key];
      for (const alias of field.aliases ?? []) delete next[alias];
      const encoded = field.encode(state[stateKey]);
      if (encoded !== undefined) next[field.key] = encoded;
    }
    return next;
  }

  return { decode, encode };
}

export interface StringQueryFieldOptions {
  aliases?: readonly string[];
  defaultValue?: string;
  validate?: (value: string) => boolean;
}

export function stringQueryField(
  key: string,
  options: StringQueryFieldOptions = {},
): WorkspaceQueryField<string> {
  const defaultValue = options.defaultValue ?? "";
  return {
    key,
    aliases: options.aliases,
    decode(value) {
      if (value === undefined || (options.validate && !options.validate(value))) return defaultValue;
      return value;
    },
    encode(value) {
      return value && value !== defaultValue && (!options.validate || options.validate(value)) ? value : undefined;
    },
  };
}

export interface IntegerQueryFieldOptions {
  aliases?: readonly string[];
  defaultValue?: number;
  min?: number;
  max?: number;
}

export function integerQueryField(
  key: string,
  options: IntegerQueryFieldOptions = {},
): WorkspaceQueryField<number> {
  const defaultValue = options.defaultValue ?? 1;
  const min = options.min ?? Number.MIN_SAFE_INTEGER;
  const max = options.max ?? Number.MAX_SAFE_INTEGER;
  const normalize = (value: string | undefined) => {
    const parsed = value === undefined ? Number.NaN : Number(value);
    return Number.isInteger(parsed) && parsed >= min && parsed <= max ? parsed : defaultValue;
  };
  return {
    key,
    aliases: options.aliases,
    decode: normalize,
    encode(value) {
      return Number.isInteger(value) && value >= min && value <= max && value !== defaultValue
        ? String(value)
        : undefined;
    },
  };
}

export function enumQueryField<T extends string>(
  key: string,
  values: readonly T[],
  defaultValue: T,
  aliases: readonly string[] = [],
): WorkspaceQueryField<T> {
  const allowed = new Set<string>(values);
  return {
    key,
    aliases,
    decode(value) {
      return value !== undefined && allowed.has(value) ? value as T : defaultValue;
    },
    encode(value) {
      return allowed.has(value) && value !== defaultValue ? value : undefined;
    },
  };
}
