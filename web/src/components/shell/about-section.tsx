"use client";

import { useSyncExternalStore } from "react";

import type { SectionAbout } from "@/content/sections";

const STORAGE_PREFIX = "shogun.about.";
const CLOSED = "closed";

// closedHere remembers closed explainers for this tab, so the choice holds even
// when storage is unavailable, for example in a private window.
const closedHere = new Set<string>();
const listeners = new Set<() => void>();

function storageKey(id: string): string {
  return STORAGE_PREFIX + id;
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  window.addEventListener("storage", listener);
  return () => {
    listeners.delete(listener);
    window.removeEventListener("storage", listener);
  };
}

function isClosed(id: string): boolean {
  if (closedHere.has(id)) {
    return true;
  }
  try {
    return window.localStorage.getItem(storageKey(id)) === CLOSED;
  } catch {
    return false;
  }
}

function setClosed(id: string, closed: boolean): void {
  if (closed) {
    closedHere.add(id);
  } else {
    closedHere.delete(id);
  }
  try {
    if (closed) {
      window.localStorage.setItem(storageKey(id), CLOSED);
    } else {
      window.localStorage.removeItem(storageKey(id));
    }
  } catch {
    // The choice is only kept for this tab.
  }
  listeners.forEach((listener) => listener());
}

interface AboutSectionProps {
  about: SectionAbout;
}

// AboutSection explains a section: what it is responsible for, what it does and
// how it helps. It starts open, and stays closed once the user closes it. The
// server always renders it open; the stored choice applies after hydration.
export function AboutSection({ about }: AboutSectionProps) {
  const closed = useSyncExternalStore(
    subscribe,
    () => isClosed(about.id),
    () => false,
  );

  return (
    <details open={!closed} data-testid={`about-${about.id}`} className="border-b border-ink bg-strip">
      <summary
        className="label min-h-11 cursor-pointer px-[22px] py-3 font-medium"
        onClick={(event) => {
          // React owns the open state, so the browser must not also toggle it.
          event.preventDefault();
          setClosed(about.id, !closed);
        }}
      >
        About this section
      </summary>
      <dl className="grid grid-cols-3 gap-6 px-[22px] pb-5 max-[860px]:grid-cols-1">
        <div>
          <dt className="label mb-1.5 font-medium">Responsibility</dt>
          <dd className="text-muted-ink">{about.role}</dd>
        </div>
        <div>
          <dt className="label mb-1.5 font-medium">What it does</dt>
          <dd className="text-muted-ink">{about.does}</dd>
        </div>
        <div>
          <dt className="label mb-1.5 font-medium">How it helps you</dt>
          <dd className="text-muted-ink">{about.helps}</dd>
        </div>
      </dl>
    </details>
  );
}
