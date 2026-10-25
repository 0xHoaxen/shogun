"use client";

import { ConnectError, createClient } from "@connectrpc/connect";
import { useQuery, useTransport } from "@connectrpc/connect-query";
import { useMemo, useRef, useState } from "react";

import { ImportChoose, MAX_CSV_BYTES } from "@/components/contacts/import-choose";
import { MappingTable, RowList } from "@/components/contacts/import-check";
import { importErrorText } from "@/components/contacts/import-errors";
import { choicesFrom, defaultTicks, hasName, mappingsFrom, people, repeatedHeaders, selectionRows } from "@/components/contacts/import-fields";
import { ImportReading } from "@/components/contacts/import-reading";
import { SAMPLE_CSV, SAMPLE_FILENAME } from "@/components/contacts/import-sample";
import { readSheet, type SheetInfo } from "@/components/contacts/csv-sheet";
import type { ImportJob } from "@/components/contacts/use-import-people";
import { Button } from "@/components/ui/button";
import { SidePanel } from "@/components/ui/side-panel";
import { StepHeader } from "@/components/ui/step-header";
import { ContactsService, type ImportContactsResponse } from "@/gen/shogun/api/v1/contacts_pb";
import { ProviderService } from "@/gen/shogun/api/v1/providers_pb";

const STEPS = ["Choose file", "AI reads it", "Check and import"] as const;

interface Loaded {
  filename: string;
  bytes: Uint8Array;
  sheet: SheetInfo;
}

// What step 3 shows: the file, what the last dry run said, and the owner's edits.
interface Checking extends Loaded {
  response: ImportContactsResponse;
  choices: string[];
  ticked: Set<number>;
}

type Stage = { kind: "choose" } | { kind: "reading"; loaded: Loaded } | { kind: "check"; check: Checking };

function plural(n: number, noun: string): string {
  return `${n} ${noun}${n === 1 ? "" : "s"}`;
}

interface ImportPanelProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  // onImport is given the import to run once the Undo time is up.
  onImport: (job: ImportJob) => void;
}

// ImportPanel is the bulk import of people from a CSV: choose a file, have the
// columns read, check who will be imported. Nothing is written here; the import
// is handed back to the People screen to run behind an Undo.
export function ImportPanel({ open, onOpenChange, onImport }: ImportPanelProps) {
  const transport = useTransport();
  const client = useMemo(() => createClient(ContactsService, transport), [transport]);
  const provider = useQuery(ProviderService.method.getProvider, {}, { enabled: open });
  const aiMode = provider.isSuccess && provider.data.provider !== undefined;
  const [stage, setStage] = useState<Stage>({ kind: "choose" });
  const [problem, setProblem] = useState<string | null>(null);
  const [aiFailed, setAiFailed] = useState(false);
  const [busy, setBusy] = useState(false);
  // Only the last edit of the mapping counts.
  const latest = useRef(0);

  function close() {
    latest.current++;
    setStage({ kind: "choose" });
    setProblem(null);
    setAiFailed(false);
    setBusy(false);
    onOpenChange(false);
  }

  async function read(filename: string, bytes: Uint8Array) {
    setProblem(null);
    if (bytes.byteLength > MAX_CSV_BYTES) {
      setProblem("That file is larger than 3 MB.");
      return;
    }
    const sheet = readSheet(new TextDecoder().decode(bytes));
    if (sheet === undefined || sheet.rows === 0) {
      setProblem("That file has no rows to import.");
      return;
    }
    const loaded: Loaded = { filename, bytes, sheet };
    const reshape = aiMode;
    const token = ++latest.current;
    setStage({ kind: "reading", loaded });
    try {
      const response = await client.importContacts({ filename, csv: bytes, dryRun: true, reshape });
      if (token !== latest.current) return;
      setStage({
        kind: "check",
        check: {
          ...loaded,
          response,
          choices: choicesFrom(sheet.header, response.mappings),
          ticked: defaultTicks(response.rows),
        },
      });
    } catch (caught) {
      if (token !== latest.current) return;
      setProblem(importErrorText(ConnectError.from(caught)));
      setAiFailed(reshape);
      setStage({ kind: "choose" });
    }
  }

  async function chooseFile(file: File) {
    await read(file.name, new Uint8Array(await file.arrayBuffer()));
  }

  async function changeColumn(check: Checking, column: number, target: string) {
    const choices = check.choices.map((current, i) => (i === column ? target : current));
    setProblem(null);
    if (!hasName(choices)) {
      // The list cannot be read without a name; keep the choice and say so.
      setStage({ kind: "check", check: { ...check, choices } });
      return;
    }
    const token = ++latest.current;
    setBusy(true);
    try {
      const response = await client.importContacts({
        filename: check.filename,
        csv: check.bytes,
        dryRun: true,
        mappings: mappingsFrom(check.sheet.header, choices),
        dateOrder: check.response.dateOrder,
      });
      if (token !== latest.current) return;
      setStage({ kind: "check", check: { ...check, response, choices, ticked: defaultTicks(response.rows) } });
    } catch (caught) {
      if (token !== latest.current) return;
      setProblem(importErrorText(ConnectError.from(caught)));
      setStage({ kind: "check", check: { ...check, choices } });
    } finally {
      if (token === latest.current) setBusy(false);
    }
  }

  function toggle(check: Checking, row: number) {
    const ticked = new Set(check.ticked);
    if (ticked.has(row)) ticked.delete(row);
    else ticked.add(row);
    setStage({ kind: "check", check: { ...check, ticked } });
  }

  function confirm(check: Checking) {
    const { response, sheet } = check;
    const total = response.report?.rowsTotal ?? 0;
    const rows = selectionRows(check.ticked, response.rows, total);
    const mapped = response.mappings.length > 0 || check.choices.some((choice) => choice !== "");
    onImport({
      count: rows.length,
      request: {
        filename: check.filename,
        csv: check.bytes,
        dryRun: false,
        mappings: mapped ? mappingsFrom(sheet.header, check.choices) : [],
        dateOrder: response.dateOrder,
        selection: { rows },
      },
    });
    close();
  }

  const stepIndex = stage.kind === "choose" ? 0 : stage.kind === "reading" ? 1 : 2;
  return (
    <SidePanel open={open} onOpenChange={(next) => (next ? onOpenChange(true) : close())} eyebrow="Bulk import">
      <h2 className="h-display text-3xl">Import people</h2>
      <p className="text-muted-foreground">
        Any CSV works: a LinkedIn connections export, a spreadsheet, a CRM dump. Shogun reads the columns and works out
        what each one means.
      </p>
      <StepHeader steps={STEPS} current={stepIndex} />

      {stage.kind === "choose" ? (
        <ImportChoose
          aiMode={aiMode}
          loadingProvider={provider.isPending}
          showTemplate={!provider.isPending && (!aiMode || aiFailed)}
          problem={problem}
          onFile={chooseFile}
          onSample={() => read(SAMPLE_FILENAME, new TextEncoder().encode(SAMPLE_CSV))}
        />
      ) : null}

      {stage.kind === "reading" ? (
        <ImportReading
          filename={stage.loaded.filename}
          columns={stage.loaded.sheet.header.length}
          rows={stage.loaded.sheet.rows}
          aiMode={aiMode}
        />
      ) : null}

      {stage.kind === "check" ? <CheckStep check={stage.check} aiMode={aiMode} busy={busy} problem={problem} onChange={changeColumn} onToggle={toggle} onConfirm={confirm} onAnother={() => setStage({ kind: "choose" })} /> : null}
    </SidePanel>
  );
}

interface CheckStepProps {
  check: Checking;
  aiMode: boolean;
  busy: boolean;
  problem: string | null;
  onChange: (check: Checking, column: number, target: string) => void;
  onToggle: (check: Checking, row: number) => void;
  onConfirm: (check: Checking) => void;
  onAnother: () => void;
}

function CheckStep({ check, aiMode, busy, problem, onChange, onToggle, onConfirm, onAnother }: CheckStepProps) {
  const { response, sheet } = check;
  const total = response.report?.rowsTotal ?? 0;
  const mapped = response.mappings.length > 0 || check.choices.some((choice) => choice !== "");
  const named = hasName(check.choices);
  const repeated = repeatedHeaders(sheet.header);
  const selected = selectionRows(check.ticked, response.rows, total).length;
  const past = Math.max(0, total - response.rows.length);
  const canImport = (!mapped || named) && selected > 0 && !busy;

  return (
    <div className="flex flex-col gap-4">
      <p className="label flex flex-wrap justify-between gap-2 text-muted-foreground">
        <span>{aiMode && mapped ? "Matched by AI" : "Checked"}</span>
        <span className="normal-case">
          {check.filename} · {plural(total, "row")}
        </span>
      </p>
      {mapped ? (
        <>
          {repeated.length > 0 ? (
            <p role="note">Two columns are both called {repeated.map((name) => `"${name}"`).join(", ")}, so they cannot be changed here.</p>
          ) : null}
          <MappingTable
            header={sheet.header}
            example={sheet.example}
            choices={check.choices}
            busy={busy || repeated.length > 0}
            onChange={(column, target) => onChange(check, column, target)}
          />
          {!named ? <p role="alert">Pick the column that holds the name.</p> : null}
        </>
      ) : null}
      {busy ? <p role="status">Reading the file again with your choice…</p> : null}
      {!mapped || named ? (
        <>
          <p className="label text-muted-foreground" data-testid="selected-count">
            {selected} of {total} people selected
          </p>
          <RowList rows={response.rows} ticked={check.ticked} onToggle={(row) => onToggle(check, row)} />
          {response.rowsTruncated || past > 0 ? (
            <p role="note">
              Showing the first {response.rows.length.toLocaleString("en-US")} of {total.toLocaleString("en-US")}. The rest are imported
              without a check.
            </p>
          ) : null}
        </>
      ) : null}
      {problem ? (
        <p role="alert" className="border border-line2 bg-panel px-3 py-2 wrap-anywhere">
          {problem}
        </p>
      ) : null}
      <p className="text-muted-foreground">Types are a guess from each title. You can change any person later. Nothing is sent.</p>
      <div className="flex flex-wrap justify-between gap-3">
        <Button onClick={onAnother}>Choose another file</Button>
        <Button variant="primary" disabled={!canImport} onClick={() => onConfirm(check)}>
          Import {people(selected)}
        </Button>
      </div>
    </div>
  );
}
