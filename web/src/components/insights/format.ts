// percent writes a fraction from 0 to 1 as a whole percentage.
export function percent(fraction: number): string {
  return `${Math.round(fraction * 100)}%`;
}

// RANGES are the choices for how far back to look; days is 0 for a custom range.
export const RANGES = [
  { id: "30", label: "Last 30 days", days: 30 },
  { id: "90", label: "Last 90 days", days: 90 },
  { id: "365", label: "Last year", days: 365 },
] as const;

export type RangeId = (typeof RANGES)[number]["id"];

const DAY_MS = 24 * 60 * 60 * 1000;
// IST_OFFSET_MS is the offset of Asia/Kolkata, the zone days are counted in.
const IST_OFFSET_MS = 5.5 * 60 * 60 * 1000;

// isoDay writes a date as YYYY-MM-DD.
function isoDay(ms: number): string {
  return new Date(ms).toISOString().slice(0, 10);
}

// rangeFor returns the from and to days (YYYY-MM-DD, India time, both included)
// of the last `days` days ending today.
export function rangeFor(days: number, now: Date = new Date()): { from: string; to: string } {
  const today = now.getTime() + IST_OFFSET_MS;
  return { from: isoDay(today - (days - 1) * DAY_MS), to: isoDay(today) };
}
