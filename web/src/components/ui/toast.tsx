"use client";

import { type ReactNode, createContext, useCallback, useContext, useEffect, useMemo, useState } from "react";

// How long a toast stays: long enough to notice it and reach for Undo.
export const TOAST_MS = 10_000;

interface ToastRequest {
  // Says what happened, for example "5 people imported".
  text: string;
  // When set the toast offers Undo, and calls this if it is pressed in time.
  undo?: () => void;
}

interface ToastApi {
  show: (toast: ToastRequest) => void;
}

const ToastContext = createContext<ToastApi | null>(null);

export function useToast(): ToastApi {
  const api = useContext(ToastContext);
  if (api === null) {
    throw new Error("useToast needs a ToastProvider above it");
  }
  return api;
}

interface ActiveToast extends ToastRequest {
  // Distinguishes two toasts with the same text, so each gets its own timer.
  key: number;
}

// Shows one toast at a time at the bottom of the screen; a new one replaces the old.
export function ToastProvider({ children }: { children: ReactNode }) {
  const [active, setActive] = useState<ActiveToast | null>(null);

  const show = useCallback((toast: ToastRequest) => {
    setActive((previous) => ({ ...toast, key: (previous?.key ?? 0) + 1 }));
  }, []);
  const api = useMemo(() => ({ show }), [show]);

  useEffect(() => {
    if (active === null) return;
    const timer = setTimeout(() => setActive(null), TOAST_MS);
    return () => clearTimeout(timer);
  }, [active]);

  return (
    <ToastContext.Provider value={api}>
      {children}
      <div role="status" aria-live="polite" className="contents">
        {active ? (
          <div
            key={active.key}
            className="fixed right-6 bottom-6 z-50 flex items-center gap-3 bg-cream py-2.5 pr-3 pl-3.5 text-on-accent [clip-path:polygon(8px_0,100%_0,100%_calc(100%-8px),calc(100%-8px)_100%,0_100%,0_8px)] max-[520px]:right-3 max-[520px]:bottom-3 max-[520px]:left-3"
          >
            <span className="min-w-0">{active.text}</span>
            {active.undo ? (
              <button
                type="button"
                onClick={() => {
                  active.undo?.();
                  setActive(null);
                }}
                className="min-h-[26px] cursor-pointer border border-on-accent/40 px-2.5 text-xs font-semibold"
              >
                Undo
              </button>
            ) : null}
          </div>
        ) : null}
      </div>
    </ToastContext.Provider>
  );
}
