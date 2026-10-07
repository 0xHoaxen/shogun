import type { LearningActivity } from "@/gen/shogun/api/v1/learning_pb";

const HEADINGS = ["", "Day", "What you learned", "Part of", "Minutes"] as const;

interface ActivitiesTableProps {
  activities: readonly LearningActivity[];
  // itemTitles names the items the activities belong to, by id.
  itemTitles: ReadonlyMap<string, string>;
  selected: ReadonlySet<string>;
  // atLimit stops further activities being chosen.
  atLimit: boolean;
  onToggle: (id: string) => void;
}

export function ActivitiesTable({ activities, itemTitles, selected, atLimit, onToggle }: ActivitiesTableProps) {
  return (
    <div className="overflow-x-auto border-b border-ink">
      <table className="w-full min-w-[760px] border-collapse text-left">
        <caption className="sr-only">Learning activities</caption>
        <thead>
          <tr className="label h-9 bg-ink text-paper">
            {HEADINGS.map((heading, index) => (
              <th key={`${heading}-${index}`} className="px-2.5 font-medium">
                {heading}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {activities.map((activity) => (
            <tr key={activity.id} className="border-b border-ink align-top">
              <td className="w-10 px-2.5 py-2.5">
                <input
                  type="checkbox"
                  aria-label={`Choose: ${activity.summary}`}
                  checked={selected.has(activity.id)}
                  disabled={atLimit && !selected.has(activity.id)}
                  onChange={() => onToggle(activity.id)}
                />
              </td>
              <td className="px-2.5 py-2.5 whitespace-nowrap">{activity.occurredOn}</td>
              <td className="max-w-[420px] px-2.5 py-2.5">
                {activity.summary}
                {activity.tags.length > 0 ? (
                  <>
                    <br />
                    <span className="text-[11px] text-muted-ink">{activity.tags.join(", ")}</span>
                  </>
                ) : null}
              </td>
              <td className="px-2.5 py-2.5">{itemTitles.get(activity.itemId) ?? "-"}</td>
              <td className="px-2.5 py-2.5">{activity.minutes > 0 ? activity.minutes : "-"}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
