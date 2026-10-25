import { ProgressLines, type ProgressLine } from "@/components/ui/progress-lines";

interface ImportReadingProps {
  filename: string;
  columns: number;
  rows: number;
  // aiMode is true when a provider matches the columns; otherwise the file is
  // only checked.
  aiMode: boolean;
}

// ImportReading is step 2: where the one request is. The first lines are the
// browser's own reading of the header and are done at once; the others are the
// request in progress. It makes up no progress of its own.
export function ImportReading({ filename, columns, rows, aiMode }: ImportReadingProps) {
  const lines: ProgressLine[] = [
    { text: `Reading ${filename}`, state: "done" },
    { text: `Found ${columns} columns and ${rows.toLocaleString("en-US")} rows`, state: "done" },
    aiMode
      ? { text: "Matching columns to name, email, company, role", state: "now" }
      : { text: "Checking the file against the template columns", state: "now" },
  ];
  if (aiMode) {
    lines.push({ text: "Guessing who is a recruiter, an engineer or a manager", state: "pending" });
  }
  return <ProgressLines lines={lines} />;
}
