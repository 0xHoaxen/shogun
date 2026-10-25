import type { ColumnMapping } from "@/gen/shogun/api/v1/contacts_pb";

// ImportAiHint replaces the column template when the owner has an AI provider:
// any sheet works, so it only says what each person needs.
export function ImportAiHint() {
  return (
    <div data-testid="import-ai-hint" className="grid gap-2 border border-line2 bg-bg p-3 text-xs">
      <p>
        Any contacts sheet works, such as a LinkedIn or Google export. Each person needs a{" "}
        <strong>name</strong>; nothing else is required.
      </p>
      <p className="text-muted-foreground">
        Company, role, email, LinkedIn, X handle, phone, how you met, status, dates, tags and notes
        are picked up when the sheet has them. Your AI provider matches the columns, and you check
        the result before anything is saved.
      </p>
    </div>
  );
}

// ColumnMappings shows which of the sheet's columns fill each field, so the
// owner can catch a wrong match before confirming.
export function ColumnMappings({ mappings }: { mappings: ColumnMapping[] }) {
  if (mappings.length === 0) {
    return null;
  }
  return (
    <div className="grid gap-1.5">
      <p>Your AI provider matched these columns. Check them before importing.</p>
      <table aria-label="Matched columns" className="w-full border-collapse border border-line2 text-left">
        <thead>
          <tr className="label bg-cream text-on-accent">
            <th className="px-2.5 py-1.5 font-medium">Your sheet</th>
            <th className="px-2.5 py-1.5 font-medium">Field</th>
          </tr>
        </thead>
        <tbody>
          {mappings.map((mapping) => (
            <tr key={mapping.target} className="border-t border-line2">
              <td className="px-2.5 py-1.5 wrap-anywhere">{mapping.sources.join(" + ")}</td>
              <td className="px-2.5 py-1.5">
                <code>{mapping.target}</code>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
