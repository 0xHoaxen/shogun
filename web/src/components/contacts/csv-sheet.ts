// A small CSV reader for the import panel, which needs a sheet's header, its
// first row and how many rows it has before anything is sent. kagami reads the
// file for real; this only has to agree with it about which row is the header.

const BOM = "﻿";
// kagami looks at this many records at the top for the header (some exports,
// such as LinkedIn's, put notes first).
const HEADER_SEARCH_ROWS = 10;
const EXAMPLE_MAX_CHARS = 60;

export interface SheetInfo {
  header: string[];
  // example is the first filled row below the header, one cell per column.
  example: string[];
  // rows counts the filled rows below the header.
  rows: number;
}

// parseCsv reads quoted cells (with "" for a quote and line breaks inside
// quotes), commas, and LF, CRLF or CR line ends. Ragged rows are kept as they are.
export function parseCsv(text: string): string[][] {
  const input = text.startsWith(BOM) ? text.slice(1) : text;
  const records: string[][] = [];
  let record: string[] = [];
  let cell = "";
  let quoted = false;
  for (let i = 0; i < input.length; i++) {
    const ch = input[i];
    if (quoted) {
      if (ch === '"' && input[i + 1] === '"') {
        cell += '"';
        i++;
      } else if (ch === '"') {
        quoted = false;
      } else {
        cell += ch;
      }
      continue;
    }
    if (ch === '"' && cell === "") {
      quoted = true;
    } else if (ch === ",") {
      record.push(cell);
      cell = "";
    } else if (ch === "\n" || ch === "\r") {
      if (ch === "\r" && input[i + 1] === "\n") {
        i++;
      }
      record.push(cell);
      records.push(record);
      record = [];
      cell = "";
    } else {
      cell += ch;
    }
  }
  if (cell !== "" || record.length > 0) {
    record.push(cell);
    records.push(record);
  }
  return records;
}

function filled(record: readonly string[]): number {
  return record.filter((cell) => cell.trim() !== "").length;
}

// readSheet finds the header the way kagami does: the first of the top records
// with the most filled cells.
export function readSheet(text: string): SheetInfo | undefined {
  const records = parseCsv(text);
  if (records.length === 0) {
    return undefined;
  }
  let at = 0;
  let best = -1;
  for (let i = 0; i < records.length && i < HEADER_SEARCH_ROWS; i++) {
    const count = filled(records[i]);
    if (count > best) {
      at = i;
      best = count;
    }
  }
  const header = records[at].map((name) => name.trim());
  const body = records.slice(at + 1).filter((record) => filled(record) > 0);
  const first = body[0] ?? [];
  const example = header.map((_, i) => clip((first[i] ?? "").trim()));
  return { header, example, rows: body.length };
}

function clip(cell: string): string {
  const chars = [...cell];
  return chars.length > EXAMPLE_MAX_CHARS ? `${chars.slice(0, EXAMPLE_MAX_CHARS - 1).join("")}…` : cell;
}
