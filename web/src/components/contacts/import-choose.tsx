"use client";

import { useState, type ChangeEvent, type DragEvent } from "react";

import { ImportFormat } from "@/components/contacts/import-format";
import { Button } from "@/components/ui/button";

// Torii and kagami both refuse larger files.
export const MAX_CSV_BYTES = 3 << 20;
export const MAX_ROWS = 5000;

interface ImportChooseProps {
  // aiMode is true when the owner has an AI provider, so any sheet will do.
  aiMode: boolean;
  // loadingProvider is true until it is known whether there is a provider.
  loadingProvider: boolean;
  // showTemplate is true when the file has to use our columns.
  showTemplate: boolean;
  problem: string | null;
  onFile: (file: File) => void;
  onSample: () => void;
}

// ImportChoose is step 1: pick a file.
export function ImportChoose({ aiMode, loadingProvider, showTemplate, problem, onFile, onSample }: ImportChooseProps) {
  const [over, setOver] = useState(false);

  function pick(event: ChangeEvent<HTMLInputElement>) {
    const file = event.target.files?.[0];
    event.target.value = "";
    if (file) onFile(file);
  }

  function drop(event: DragEvent<HTMLLabelElement>) {
    event.preventDefault();
    setOver(false);
    const file = event.dataTransfer.files?.[0];
    if (file && !loadingProvider) onFile(file);
  }

  return (
    <div className="flex flex-col gap-4">
      <label
        htmlFor="contacts-csv"
        onDragOver={(event) => {
          event.preventDefault();
          setOver(true);
        }}
        onDragLeave={() => setOver(false)}
        onDrop={drop}
        className={`flex cursor-pointer flex-col items-center gap-2 border border-dashed p-6 text-center ${over ? "border-scheme bg-panel" : "border-line2"}`}
      >
        <span className="font-medium">Drop a CSV here or choose a file</span>
        <span className="text-muted-foreground">
          Up to {MAX_ROWS.toLocaleString("en-US")} rows. Stays on your device until you confirm.
        </span>
        <input
          id="contacts-csv"
          aria-label="CSV file"
          type="file"
          accept=".csv,text/csv"
          // Until the provider is known it is not clear whether to reshape.
          disabled={loadingProvider}
          onChange={pick}
          className="sr-only"
        />
      </label>
      {problem ? (
        <p role="alert" className="border border-line2 bg-panel px-3 py-2 wrap-anywhere">
          {problem}
        </p>
      ) : null}
      {aiMode ? (
        <div className="flex flex-wrap items-center gap-3">
          <span className="text-muted-foreground">No file handy?</span>
          <Button onClick={onSample}>Use a sample LinkedIn export</Button>
        </div>
      ) : null}
      {showTemplate ? <ImportFormat /> : null}
    </div>
  );
}
