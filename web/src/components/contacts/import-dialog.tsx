"use client";

import { create } from "@bufbuild/protobuf";
import { createClient } from "@connectrpc/connect";
import { useQuery, useTransport } from "@connectrpc/connect-query";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useMemo, useState, type ChangeEvent } from "react";

import { contactsQueryKey } from "@/components/contacts/contacts-key";
import { ColumnMappings, ImportAiHint } from "@/components/contacts/import-ai";
import { importErrorText } from "@/components/contacts/import-errors";
import { ImportFormat } from "@/components/contacts/import-format";
import { Button } from "@/components/ui/button";
import { Dialog } from "@/components/ui/dialog";
import {
  ContactsService,
  ImportReportSchema,
  type ColumnMapping,
  type ImportReport,
} from "@/gen/shogun/api/v1/contacts_pb";
import { ProviderService } from "@/gen/shogun/api/v1/providers_pb";

// Torii and kagami both refuse larger files.
const MAX_CSV_BYTES = 3 << 20;

type Stage =
  | { kind: "choose" }
  | {
      kind: "preview";
      filename: string;
      // csv is what Confirm imports: the AI-shaped sheet when there is one.
      csv: Uint8Array;
      report: ImportReport;
      mappings: ColumnMapping[];
    }
  | { kind: "done"; report: ImportReport };

interface ImportVariables {
  filename: string;
  csv: Uint8Array;
  dryRun: boolean;
  reshape?: boolean;
}

function plural(count: number, noun: string): string {
  return `${count} ${noun}${count === 1 ? "" : "s"}`;
}

function PreviewSummary({ filename, report }: { filename: string; report: ImportReport }) {
  return (
    <p>
      {filename} has {plural(report.rowsTotal, "row")}. {report.rowsCreated} are new,{" "}
      {report.rowsUpdated} update existing people, {report.rowsFailed} are skipped. Nothing is saved
      until you confirm.
    </p>
  );
}

function RowErrors({ report }: { report: ImportReport }) {
  if (report.errors.length === 0) {
    return null;
  }
  return (
    <div className="max-h-56 overflow-auto border border-line2">
      <table className="w-full border-collapse text-left">
        <thead>
          <tr className="label bg-cream text-on-accent">
            <th className="px-2.5 py-1.5 font-medium">Row</th>
            <th className="px-2.5 py-1.5 font-medium">Column</th>
            <th className="px-2.5 py-1.5 font-medium">Problem</th>
          </tr>
        </thead>
        <tbody>
          {report.errors.map((rowError) => (
            <tr key={`${rowError.row}-${rowError.column}`} className="border-t border-line2">
              <td className="px-2.5 py-1.5">{rowError.row}</td>
              <td className="px-2.5 py-1.5">{rowError.column}</td>
              <td className="px-2.5 py-1.5">{rowError.message}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

interface ImportDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

export function ImportDialog({ open, onOpenChange }: ImportDialogProps) {
  const transport = useTransport();
  const client = useMemo(() => createClient(ContactsService, transport), [transport]);
  const queryClient = useQueryClient();
  const [stage, setStage] = useState<Stage>({ kind: "choose" });
  const [problem, setProblem] = useState<string | null>(null);
  // AI mode: with a provider set any sheet is mapped by it, so the template is
  // only shown again once an AI import has failed.
  const provider = useQuery(ProviderService.method.getProvider, {}, { enabled: open });
  const aiMode = provider.isSuccess && provider.data.provider !== undefined;
  const [aiFailed, setAiFailed] = useState(false);

  const run = useMutation({
    mutationFn: (vars: ImportVariables) => client.importContacts(vars),
  });

  function close() {
    setStage({ kind: "choose" });
    setProblem(null);
    setAiFailed(false);
    run.reset();
    onOpenChange(false);
  }

  async function handleFile(event: ChangeEvent<HTMLInputElement>) {
    const file = event.target.files?.[0];
    event.target.value = "";
    if (!file) {
      return;
    }
    setProblem(null);
    if (file.size > MAX_CSV_BYTES) {
      setProblem("That file is larger than 3 MB.");
      return;
    }
    const csv = new Uint8Array(await file.arrayBuffer());
    const reshape = aiMode;
    run.mutate(
      { filename: file.name, csv, dryRun: true, reshape },
      {
        onSuccess: (response) =>
          setStage({
            kind: "preview",
            filename: file.name,
            csv: response.shapedCsv.length > 0 ? response.shapedCsv : csv,
            report: response.report ?? create(ImportReportSchema),
            mappings: response.mappings,
          }),
        onError: (error) => {
          setProblem(importErrorText(error));
          setAiFailed(reshape);
        },
      },
    );
  }

  function confirm(preview: Extract<Stage, { kind: "preview" }>) {
    setProblem(null);
    run.mutate(
      { filename: preview.filename, csv: preview.csv, dryRun: false },
      {
        onSuccess: async (response) => {
          await queryClient.invalidateQueries({ queryKey: contactsQueryKey });
          setStage({ kind: "done", report: response.report ?? preview.report });
        },
        onError: (error) => setProblem(importErrorText(error)),
      },
    );
  }

  const choosing = stage.kind === "choose";
  const showTemplate = !provider.isPending && (!aiMode || aiFailed);

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => (next ? onOpenChange(true) : close())}
      title="Import CSV"
      description="Pick your contacts sheet. You see what would change before anything is saved."
    >
      {/* minmax(0, 1fr) keeps a long unbroken server message from widening the dialog. */}
      <div className="grid grid-cols-[minmax(0,1fr)] gap-4 p-[22px]">
        {stage.kind === "choose" ? (
          <div>
            <label htmlFor="contacts-csv" className="label mb-1.5 block text-[11px] font-medium">
              CSV file
            </label>
            <input
              id="contacts-csv"
              type="file"
              accept=".csv,text/csv"
              // Until the provider is known it is not clear whether to reshape.
              disabled={run.isPending || provider.isPending}
              onChange={handleFile}
              className="block w-full min-w-0 border border-line2 bg-bg p-2 text-xs"
            />
          </div>
        ) : null}

        {choosing && aiMode && run.isPending ? (
          <p role="status">Your AI provider is matching the sheet&apos;s columns…</p>
        ) : null}

        {choosing && aiMode && !aiFailed ? <ImportAiHint /> : null}

        {choosing && showTemplate ? <ImportFormat /> : null}

        {stage.kind === "preview" ? (
          <>
            <ColumnMappings mappings={stage.mappings} />
            <PreviewSummary filename={stage.filename} report={stage.report} />
            <RowErrors report={stage.report} />
          </>
        ) : null}

        {stage.kind === "done" ? (
          <p>
            Imported {stage.report.rowsCreated} new and updated {stage.report.rowsUpdated}.
          </p>
        ) : null}

        {problem ? (
          <p role="alert" className="border border-line2 bg-panel px-3 py-2 wrap-anywhere">
            {problem}
          </p>
        ) : null}

        <div className="flex justify-end gap-2">
          {stage.kind === "done" ? (
            <Button variant="primary" onClick={close}>
              Done
            </Button>
          ) : (
            <Button onClick={close}>Cancel</Button>
          )}
          {stage.kind === "preview" ? (
            <Button
              variant="primary"
              disabled={stage.report.rowsCreated + stage.report.rowsUpdated === 0 || run.isPending}
              onClick={() => confirm(stage)}
            >
              Import {plural(stage.report.rowsCreated + stage.report.rowsUpdated, "row")}
            </Button>
          ) : null}
        </div>
      </div>
    </Dialog>
  );
}
