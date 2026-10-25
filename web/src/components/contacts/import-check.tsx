"use client";

import { useId } from "react";

import { IMPORT_FIELDS, SKIP, kindOf, type RowKind } from "@/components/contacts/import-fields";
import { RELATIONSHIP_NAMES, relationshipOf } from "@/components/contacts/relationship";
import { Select } from "@/components/ui/field";
import { Tag } from "@/components/ui/tag";
import type { ImportPreviewRow } from "@/gen/shogun/api/v1/contacts_pb";
import { cn } from "@/lib/utils";

interface MappingTableProps {
  header: readonly string[];
  example: readonly string[];
  choices: readonly string[];
  busy: boolean;
  onChange: (column: number, target: string) => void;
}

// MappingTable lists each column of the sheet, where it goes and an example, with
// a select to change it. Changing one makes the list below read again.
export function MappingTable({ header, example, choices, busy, onChange }: MappingTableProps) {
  const prefix = useId();
  return (
    <div className="overflow-x-auto">
      <table aria-label="Matched columns" className="w-full border-collapse text-left">
        <thead>
          <tr className="label text-muted-foreground">
            <th className="py-2 pr-3 font-normal">Your column</th>
            <th className="py-2 pr-3 font-normal">Goes to</th>
            <th className="py-2 font-normal">Example</th>
          </tr>
        </thead>
        <tbody>
          {header.map((name, i) => (
            <tr key={`${name}-${i}`} className="border-t border-dashed border-line2">
              <td className="py-2 pr-3 align-middle wrap-anywhere">{name || "(no name)"}</td>
              <td className="py-2 pr-3 align-middle">
                <Select
                  aria-label={`Field for ${name || `column ${i + 1}`}`}
                  id={`${prefix}-${i}`}
                  value={choices[i] ?? SKIP}
                  disabled={busy}
                  onChange={(event) => onChange(i, event.target.value)}
                >
                  <option value={SKIP}>Skip</option>
                  {IMPORT_FIELDS.map((field) => (
                    <option key={field.target} value={field.target}>
                      {field.label}
                    </option>
                  ))}
                </Select>
              </td>
              <td className="py-2 align-middle font-mono text-xs text-muted-foreground wrap-anywhere">{example[i] || "—"}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function tagFor(row: ImportPreviewRow, kind: RowKind) {
  switch (kind) {
    case "problem":
      return <Tag tone="warn">Can&apos;t import</Tag>;
    case "repeat":
      return <Tag tone="warn">Repeat of row {row.repeatsRow}</Tag>;
    case "duplicate":
      return <Tag tone="warn">Already in People</Tag>;
    default:
      return <Tag>{RELATIONSHIP_NAMES[relationshipOf(row.relationship)]}</Tag>;
  }
}

function noteFor(row: ImportPreviewRow, kind: RowKind, ticked: boolean): string {
  if (kind === "problem") return row.problem;
  if (kind === "duplicate" && ticked) return "Will update their details";
  if (kind === "repeat" && ticked) return "Will update the same person again";
  return "";
}

interface RowListProps {
  rows: readonly ImportPreviewRow[];
  ticked: ReadonlySet<number>;
  onToggle: (row: number) => void;
}

// RowList is the people in the file, each with a checkbox and a tag that says
// what importing them would do.
export function RowList({ rows, ticked, onToggle }: RowListProps) {
  return (
    <ul aria-label="People in the file" className="border border-dashed border-line2">
      {rows.map((row) => {
        const kind = kindOf(row);
        const on = ticked.has(row.row);
        const note = noteFor(row, kind, on);
        const id = `import-row-${row.row}`;
        return (
          <li key={row.row} className="border-b border-dashed border-line2 last:border-b-0">
            <label htmlFor={id} className={cn("flex items-start gap-3 px-3 py-3", kind === "problem" ? "cursor-not-allowed opacity-70" : "cursor-pointer")}>
              <input
                id={id}
                type="checkbox"
                checked={on}
                disabled={kind === "problem"}
                onChange={() => onToggle(row.row)}
                className="mt-1 size-4 accent-[var(--accent)]"
              />
              <span className="flex min-w-0 grow flex-col gap-1">
                <span className={cn("truncate", !on && "text-muted-foreground line-through")}>{row.fullName || "(no name)"}</span>
                <span className="truncate font-mono text-xs text-muted-foreground">
                  {[row.role, row.company].filter(Boolean).join(" · ") || "No role or company"}
                </span>
                {note ? <span className="text-xs text-muted-foreground">{note}</span> : null}
              </span>
              <span className="shrink-0">{tagFor(row, kind)}</span>
            </label>
          </li>
        );
      })}
    </ul>
  );
}
