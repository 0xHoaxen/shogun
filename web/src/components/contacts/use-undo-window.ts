"use client";

import { useCallback, useEffect, useRef } from "react";

import { TOAST_MS, useToast } from "@/components/ui/toast";

interface Waiting<T> {
  item: T;
  timer: ReturnType<typeof setTimeout>;
}

export interface UndoWindow<T> {
  // start shows a toast with Undo and sends item when it ends. Anything that was
  // still waiting is sent first.
  start: (item: T, text: string, onUndo?: () => void) => void;
  // flush sends what is waiting now. Call it before leaving what it is about.
  flush: () => void;
}

// useUndoWindow gives an action an Undo that really undoes it: the call is not
// made until the toast's time is up, so Undo only has to cancel it. Something
// still waiting is sent at once when another starts, the tab is hidden, the page
// goes or the screen is left, so it is never lost.
export function useUndoWindow<T>(send: (item: T) => void | Promise<void>): UndoWindow<T> {
  const toast = useToast();
  const waiting = useRef<Waiting<T> | null>(null);
  // The latest send, so a call made on leaving the screen uses current state.
  const sendRef = useRef(send);
  useEffect(() => {
    sendRef.current = send;
  }, [send]);

  const flush = useCallback(() => {
    const entry = waiting.current;
    if (entry === null) {
      return;
    }
    clearTimeout(entry.timer);
    waiting.current = null;
    void sendRef.current(entry.item);
  }, []);

  const start = useCallback<UndoWindow<T>["start"]>(
    (item, text, onUndo) => {
      flush();
      const entry: Waiting<T> = { item, timer: setTimeout(flush, TOAST_MS) };
      waiting.current = entry;
      toast.show({
        text,
        undo: () => {
          if (waiting.current === entry) {
            clearTimeout(entry.timer);
            waiting.current = null;
            onUndo?.();
          }
        },
      });
    },
    [flush, toast],
  );

  useEffect(() => {
    const onHide = () => {
      if (document.visibilityState === "hidden") {
        flush();
      }
    };
    document.addEventListener("visibilitychange", onHide);
    window.addEventListener("pagehide", flush);
    return () => {
      document.removeEventListener("visibilitychange", onHide);
      window.removeEventListener("pagehide", flush);
      flush();
    };
  }, [flush]);

  return { start, flush };
}
