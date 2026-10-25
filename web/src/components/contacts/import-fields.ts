import type { ColumnMapping, ImportPreviewRow } from "@/gen/shogun/api/v1/contacts_pb";

// The import columns kagami accepts, with the names the panel shows. The order
// is the template's.
export const IMPORT_FIELDS = [
  { target: "full_name", label: "Name" },
  { target: "company", label: "Company" },
  { target: "role", label: "Role" },
  { target: "email", label: "Email" },
  { target: "linkedin_url", label: "LinkedIn" },
  { target: "x_handle", label: "X handle" },
  { target: "phone", label: "Phone" },
  { target: "relationship", label: "Relationship" },
  { target: "how_we_met", label: "How you know them" },
  { target: "status", label: "Status" },
  { target: "preferred_channel", label: "Preferred channel" },
  { target: "last_contacted", label: "Last contacted" },
  { target: "next_follow_up", label: "Next follow-up" },
  { target: "target_role", label: "Target role" },
  { target: "job_link", label: "Job link" },
  { target: "tags", label: "Tags" },
  { target: "notes", label: "Notes" },
] as const;

// people is a count of people in words: "1 person", "5 people".
export function people(n: number): string {
  return `${n} ${n === 1 ? "person" : "people"}`;
}

export const SKIP = "";
export const NAME_TARGET = "full_name";

export function fieldLabel(target: string): string {
  return IMPORT_FIELDS.find((field) => field.target === target)?.label ?? target;
}

// choicesFrom turns a mapping into one choice per column of the sheet: the
// field the column fills, or SKIP. A column that is in no mapping is skipped.
export function choicesFrom(header: readonly string[], mappings: readonly Pick<ColumnMapping, "target" | "sources">[]): string[] {
  const choices = header.map(() => SKIP);
  for (const mapping of mappings) {
    for (const source of mapping.sources) {
      const at = header.indexOf(source);
      if (at >= 0) {
        choices[at] = mapping.target;
      }
    }
  }
  return choices;
}

// mappingsFrom is the reverse: one mapping per field, its sources in the
// order the columns appear in the sheet. Several columns may fill one field
// (First name and Last name into Name). Skipped columns are left out.
export function mappingsFrom(header: readonly string[], choices: readonly string[]): { target: string; sources: string[] }[] {
  const byTarget = new Map<string, string[]>();
  header.forEach((name, i) => {
    const target = choices[i] ?? SKIP;
    if (target !== SKIP) {
      byTarget.set(target, [...(byTarget.get(target) ?? []), name]);
    }
  });
  return IMPORT_FIELDS.filter((field) => byTarget.has(field.target)).map((field) => ({
    target: field.target,
    sources: byTarget.get(field.target) ?? [],
  }));
}

// hasName says whether some column fills Name, which every person needs.
export function hasName(choices: readonly string[]): boolean {
  return choices.includes(NAME_TARGET);
}

// A header is only unique if no two columns share a name: the mapping names its
// sources by header text, so a repeated name cannot be told apart.
export function repeatedHeaders(header: readonly string[]): string[] {
  return header.filter((name, i) => name !== "" && header.indexOf(name) !== i);
}

export type RowKind = "problem" | "repeat" | "duplicate" | "new";

export function kindOf(row: Pick<ImportPreviewRow, "problem" | "repeatsRow" | "duplicate">): RowKind {
  if (row.problem !== "") return "problem";
  if (row.repeatsRow > 0) return "repeat";
  if (row.duplicate) return "duplicate";
  return "new";
}

// defaultTicks is who is ticked at first: people who are new. Someone already in
// the book, a repeat of an earlier row, or a row that cannot be imported starts
// unticked.
export function defaultTicks(rows: readonly ImportPreviewRow[]): Set<number> {
  return new Set(rows.filter((row) => kindOf(row) === "new").map((row) => row.row));
}

// selectionRows is what to send as the selection: the ticked rows, and every
// row past the preview, which is imported without a check. The first row of
// data is CSV row 2 (the header is row 1); total is the number of data rows.
export function selectionRows(ticked: ReadonlySet<number>, previewed: readonly ImportPreviewRow[], total: number): number[] {
  const last = previewed.reduce((max, row) => Math.max(max, row.row), 1);
  const rest: number[] = [];
  for (let row = last + 1; row <= total + 1; row++) {
    rest.push(row);
  }
  return [...[...ticked].sort((a, b) => a - b), ...rest];
}
