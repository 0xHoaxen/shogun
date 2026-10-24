import { useCallback, useSyncExternalStore } from "react";

import type { Scheme } from "@/content/sections";

// Decision D6: how the app looks lives in this browser only, under one
// versioned key. Bump the version in the key when the shape stops being
// readable; old data is then ignored, not migrated.
export const APPEARANCE_KEY = "shogun.appearance.v1";

export type Density = "comfortable" | "compact";
export type Motion = "on" | "off";

export interface DashboardLayout {
  // Widget ids in display order, and the ones the user hid.
  order: readonly string[];
  hidden: readonly string[];
}

export interface Appearance {
  // Accent scheme per page, keyed by the page's route (for example "/people").
  // A page without an entry uses its default.
  schemes: Readonly<Record<string, Scheme>>;
  density: Density;
  // "off" stops animation. The OS setting prefers-reduced-motion stops it too,
  // whatever this says (see globals.css).
  motion: Motion;
  sidebarCollapsed: boolean;
  dashboard: DashboardLayout;
}

export const DEFAULT_APPEARANCE: Appearance = {
  schemes: {},
  density: "comfortable",
  motion: "on",
  sidebarCollapsed: false,
  dashboard: { order: [], hidden: [] },
};

const SCHEMES: readonly Scheme[] = ["ember", "moss", "tide", "plum"];

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function stringList(value: unknown): readonly string[] {
  return Array.isArray(value) ? value.filter((item): item is string => typeof item === "string") : [];
}

function schemeMap(value: unknown): Readonly<Record<string, Scheme>> {
  if (!isRecord(value)) return {};
  const entries = Object.entries(value).filter((entry): entry is [string, Scheme] =>
    SCHEMES.includes(entry[1] as Scheme),
  );
  return Object.fromEntries(entries);
}

// parseAppearance reads whatever is in storage. Anything missing, malformed or
// unknown falls back to the default for that field, so a damaged value never
// breaks the app.
export function parseAppearance(raw: string | null): Appearance {
  if (raw === null) return DEFAULT_APPEARANCE;
  let data: unknown;
  try {
    data = JSON.parse(raw);
  } catch {
    return DEFAULT_APPEARANCE;
  }
  if (!isRecord(data)) return DEFAULT_APPEARANCE;
  const dashboard = isRecord(data.dashboard) ? data.dashboard : {};
  return {
    schemes: schemeMap(data.schemes),
    density: data.density === "compact" ? "compact" : DEFAULT_APPEARANCE.density,
    motion: data.motion === "off" ? "off" : DEFAULT_APPEARANCE.motion,
    sidebarCollapsed: data.sidebarCollapsed === true,
    dashboard: { order: stringList(dashboard.order), hidden: stringList(dashboard.hidden) },
  };
}

function readStorage(): string | null {
  try {
    return window.localStorage.getItem(APPEARANCE_KEY);
  } catch {
    // Storage can be blocked (private windows, site settings).
    return null;
  }
}

function writeStorage(value: Appearance): void {
  try {
    window.localStorage.setItem(APPEARANCE_KEY, JSON.stringify(value));
  } catch {
    // Not saved; the change still applies until the page is closed.
  }
}

// The store keeps the latest value in memory, so a blocked or full storage
// still lets the settings work for this visit, and so getSnapshot returns the
// same object until something changes.
let current: Appearance | null = null;
const listeners = new Set<() => void>();

function getSnapshot(): Appearance {
  current ??= parseAppearance(readStorage());
  return current;
}

function getServerSnapshot(): Appearance {
  return DEFAULT_APPEARANCE;
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  // Another tab changed the setting.
  const onStorage = (event: StorageEvent) => {
    if (event.key !== APPEARANCE_KEY) return;
    current = parseAppearance(event.newValue);
    listener();
  };
  window.addEventListener("storage", onStorage);
  return () => {
    listeners.delete(listener);
    window.removeEventListener("storage", onStorage);
  };
}

export function updateAppearance(patch: Partial<Appearance>): void {
  current = { ...getSnapshot(), ...patch };
  writeStorage(current);
  listeners.forEach((listener) => listener());
}

export interface UseAppearance {
  appearance: Appearance;
  update: (patch: Partial<Appearance>) => void;
}

export function useAppearance(): UseAppearance {
  const appearance = useSyncExternalStore(subscribe, getSnapshot, getServerSnapshot);
  const update = useCallback((patch: Partial<Appearance>) => updateAppearance(patch), []);
  return { appearance, update };
}
