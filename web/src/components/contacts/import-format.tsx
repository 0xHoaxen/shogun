// IMPORT_COLUMNS mirror importColumns in
// services/kagami/internal/app/importcsv_parse.go; kagami refuses any other header.
const IMPORT_COLUMNS = [
  "full_name",
  "company",
  "role",
  "email",
  "linkedin_url",
  "x_handle",
  "phone",
  "relationship",
  "how_we_met",
  "status",
  "preferred_channel",
  "last_contacted",
  "next_follow_up",
  "target_role",
  "job_link",
  "tags",
  "notes",
] as const;

const EXAMPLE_ROW = [
  "Asha Nair",
  "Lumen",
  "Staff Engineer",
  "asha@example.com",
  "https://www.linkedin.com/in/asha-nair",
  "ashanair",
  "",
  "engineer",
  "Met at GopherCon",
  "not_reached",
  "linkedin",
  "",
  "2026-11-01",
  "Backend Engineer",
  "",
  "go;referral",
  "",
];

const TEMPLATE_CSV = `${IMPORT_COLUMNS.join(",")}\n${EXAMPLE_ROW.join(",")}\n`;
const TEMPLATE_HREF = `data:text/csv;charset=utf-8,${encodeURIComponent(TEMPLATE_CSV)}`;

// ImportFormat tells the owner what the sheet must look like and offers a
// filled-in template to start from.
export function ImportFormat() {
  return (
    <div className="grid gap-2 border border-ink bg-field p-3 text-xs">
      <p>
        The first row must name these {IMPORT_COLUMNS.length} columns, in this order. Only{" "}
        <code>full_name</code> needs a value; leave the rest blank if unknown.
      </p>
      <ol aria-label="Columns" className="flex flex-wrap gap-1">
        {IMPORT_COLUMNS.map((column) => (
          <li key={column} className="border border-ink bg-paper px-1.5 py-0.5">
            <code>{column}</code>
          </li>
        ))}
      </ol>
      <p className="text-muted-ink">
        Dates are YYYY-MM-DD. Separate tags with <code>;</code>. Status is one of not_reached,
        reached_out, conversation_started, replied or referral_asked.
      </p>
      <a
        href={TEMPLATE_HREF}
        download="contacts-template.csv"
        className="justify-self-start underline underline-offset-2"
      >
        Download template
      </a>
    </div>
  );
}
