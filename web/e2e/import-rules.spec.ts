import { expect, test } from "@playwright/test";

import { parseCsv, readSheet } from "../src/components/contacts/csv-sheet";
import {
  choicesFrom, defaultTicks, hasName, kindOf, mappingsFrom, people, repeatedHeaders, selectionRows,
} from "../src/components/contacts/import-fields";

test.describe("parseCsv", () => {
  test("quotes, doubled quotes, commas and line breaks inside cells", () => {
    expect(parseCsv('a,"b, c","say ""hi""","line\nbreak"\n')).toEqual([["a", "b, c", 'say "hi"', "line\nbreak"]]);
  });
  test("LF, CRLF and CR line ends, a missing final newline and a BOM", () => {
    expect(parseCsv("﻿a,b\r\nc,d\re,f")).toEqual([["a", "b"], ["c", "d"], ["e", "f"]]);
  });
  test("empty cells and ragged rows are kept as they are", () => {
    expect(parseCsv("a,,c\nd\n")).toEqual([["a", "", "c"], ["d"]]);
  });
  test("an empty file has no records", () => expect(parseCsv("")).toEqual([]));
});

test.describe("readSheet", () => {
  test("the header is the fullest of the top records, so notes above it are skipped", () => {
    const sheet = readSheet("Notes:,,,\nFirst Name,Last Name,Company,Position\nAsha,Nair,Lumen,Recruiter\n");
    expect(sheet?.header).toEqual(["First Name", "Last Name", "Company", "Position"]);
    expect(sheet?.example).toEqual(["Asha", "Nair", "Lumen", "Recruiter"]);
    expect(sheet?.rows).toBe(1);
  });
  test("blank rows are not counted and a long example is cut", () => {
    const sheet = readSheet(`Name\n\n${"x".repeat(200)}\n,\n`);
    expect(sheet?.rows).toBe(1);
    expect([...(sheet?.example[0] ?? "")]).toHaveLength(60);
    expect(sheet?.example[0].endsWith("…")).toBe(true);
  });
  test("a header with no rows below it has none", () => expect(readSheet("Name,Email\n")?.rows).toBe(0));
  test("an empty file is nothing", () => expect(readSheet("")).toBeUndefined());
  test("5,000 rows are read quickly", () => {
    const text = `Name,Email\n${Array.from({ length: 5000 }, (_, i) => `P${i},p${i}@example.com`).join("\n")}\n`;
    const start = Date.now();
    expect(readSheet(text)?.rows).toBe(5000);
    expect(Date.now() - start).toBeLessThan(500);
  });
});

const HEADER = ["First Name", "Last Name", "Company", "Position"];

test.describe("mappings and choices", () => {
  test("choices are one per column, skipping what no mapping uses", () => {
    expect(choicesFrom(HEADER, [{ target: "full_name", sources: ["First Name", "Last Name"] }, { target: "role", sources: ["Position"] }])).toEqual([
      "full_name", "full_name", "", "role",
    ]);
  });
  test("a mapping rebuilt from choices keeps column order and the field order of the template", () => {
    expect(mappingsFrom(HEADER, ["full_name", "full_name", "role", "company"])).toEqual([
      { target: "full_name", sources: ["First Name", "Last Name"] },
      { target: "company", sources: ["Position"] },
      { target: "role", sources: ["Company"] },
    ]);
  });
  test("skipped columns are left out and a name is needed", () => {
    expect(mappingsFrom(HEADER, ["", "", "company", ""])).toEqual([{ target: "company", sources: ["Company"] }]);
    expect(hasName(["", "company"])).toBe(false);
    expect(hasName(["", "full_name"])).toBe(true);
  });
  test("choices and mappings round-trip", () => {
    const choices = ["full_name", "full_name", "company", "role"];
    expect(choicesFrom(HEADER, mappingsFrom(HEADER, choices))).toEqual(choices);
  });
  test("two columns with the same name are noticed", () => {
    expect(repeatedHeaders(["Name", "Email", "Name", ""])).toEqual(["Name"]);
    expect(repeatedHeaders(["A", "B"])).toEqual([]);
  });
});

test.describe("rows", () => {
  const row = (over: object) => ({ row: 2, problem: "", repeatsRow: 0, duplicate: false, ...over }) as never;
  test("each row is one kind, a problem first", () => {
    expect(kindOf(row({ problem: "x", duplicate: true, repeatsRow: 2 }))).toBe("problem");
    expect(kindOf(row({ duplicate: true, repeatsRow: 2 }))).toBe("repeat");
    expect(kindOf(row({ duplicate: true }))).toBe("duplicate");
    expect(kindOf(row({}))).toBe("new");
  });
  test("only new people start ticked", () => {
    const ticks = defaultTicks([row({ row: 2 }), row({ row: 3, duplicate: true }), row({ row: 4, repeatsRow: 2 }), row({ row: 5, problem: "x" }), row({ row: 6 })]);
    expect([...ticks]).toEqual([2, 6]);
  });
  test("the selection is the ticked rows in order plus every row past the preview", () => {
    const shown = [row({ row: 2 }), row({ row: 3 }), row({ row: 4 })];
    expect(selectionRows(new Set([4, 2]), shown, 3)).toEqual([2, 4]);
    expect(selectionRows(new Set([2]), shown, 6)).toEqual([2, 5, 6, 7]);
    expect(selectionRows(new Set(), [], 0)).toEqual([]);
  });
  test("people in words", () => {
    expect(people(0)).toBe("0 people");
    expect(people(1)).toBe("1 person");
    expect(people(2)).toBe("2 people");
  });
});
