import { itemKindLabel, itemStatusLabel, movesFrom } from "@/components/learning/labels";
import { Button } from "@/components/ui/button";
import type { ItemStatus, LearningItem } from "@/gen/shogun/api/v1/learning_pb";

const HEADINGS = ["Item", "Kind", "Status", "Dates", ""] as const;

interface ItemsTableProps {
  items: readonly LearningItem[];
  // busyId is the item with a move in flight.
  busyId: string | null;
  onMove: (item: LearningItem, to: ItemStatus) => void;
}

function datesOf(item: LearningItem): string {
  if (item.completedOn) return `Done ${item.completedOn}`;
  if (item.startedOn) return `Since ${item.startedOn}`;
  return "-";
}

export function ItemsTable({ items, busyId, onMove }: ItemsTableProps) {
  return (
    <div className="overflow-x-auto border-b border-ink">
      <table className="w-full min-w-[760px] border-collapse text-left">
        <caption className="sr-only">Learning items</caption>
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
          {items.map((item) => (
            <tr key={item.id} className="border-b border-ink align-top">
              <td className="max-w-[360px] px-2.5 py-2.5">
                <b>{item.title}</b>
                {item.insight ? (
                  <>
                    <br />
                    <span className="text-[11px] text-muted-ink">{item.insight}</span>
                  </>
                ) : null}
              </td>
              <td className="px-2.5 py-2.5">{itemKindLabel(item.kind)}</td>
              <td className="px-2.5 py-2.5">
                <span className="label inline-flex min-h-5 items-center border border-ink px-2 text-[11px]">
                  {itemStatusLabel(item.status)}
                </span>
              </td>
              <td className="px-2.5 py-2.5">{datesOf(item)}</td>
              <td className="px-2.5 py-2.5">
                <div className="flex flex-wrap justify-end gap-2">
                  {movesFrom(item.status).map((move) => (
                    <Button
                      key={move.to}
                      aria-label={`${move.label} ${item.title}`}
                      disabled={busyId === item.id}
                      onClick={() => onMove(item, move.to)}
                    >
                      {move.label}
                    </Button>
                  ))}
                </div>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
