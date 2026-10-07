import { ItemKind, ItemStatus } from "@/gen/shogun/api/v1/learning_pb";

const KIND_LABELS: Partial<Record<ItemKind, string>> = {
  [ItemKind.COURSE]: "Course",
  [ItemKind.BOOK]: "Book",
  [ItemKind.PROJECT]: "Project",
  [ItemKind.SKILL]: "Skill",
};

const STATUS_LABELS: Partial<Record<ItemStatus, string>> = {
  [ItemStatus.PLANNED]: "Planned",
  [ItemStatus.IN_PROGRESS]: "In progress",
  [ItemStatus.DONE]: "Done",
};

export const ITEM_KINDS: readonly ItemKind[] = [ItemKind.COURSE, ItemKind.BOOK, ItemKind.PROJECT, ItemKind.SKILL];

// STATUS_FILTERS are the filters over the items; UNSPECIFIED means all.
export const STATUS_FILTERS: readonly ItemStatus[] = [
  ItemStatus.UNSPECIFIED,
  ItemStatus.PLANNED,
  ItemStatus.IN_PROGRESS,
  ItemStatus.DONE,
];

export function itemKindLabel(kind: ItemKind): string {
  return KIND_LABELS[kind] ?? "Item";
}

export function itemStatusLabel(status: ItemStatus): string {
  return STATUS_LABELS[status] ?? "All";
}

export interface ItemMove {
  to: ItemStatus;
  label: string;
}

// movesFrom lists what the owner can do to an item in a status. It mirrors the
// state machine in dojo, which is the authority: a move it refuses comes back as
// a refusal and the item is reloaded.
export function movesFrom(status: ItemStatus): readonly ItemMove[] {
  switch (status) {
    case ItemStatus.PLANNED:
      return [
        { to: ItemStatus.IN_PROGRESS, label: "Start" },
        { to: ItemStatus.DONE, label: "Finish" },
      ];
    case ItemStatus.IN_PROGRESS:
      return [
        { to: ItemStatus.DONE, label: "Finish" },
        { to: ItemStatus.PLANNED, label: "Back to planned" },
      ];
    case ItemStatus.DONE:
      return [{ to: ItemStatus.IN_PROGRESS, label: "Reopen" }];
    default:
      return [];
  }
}
