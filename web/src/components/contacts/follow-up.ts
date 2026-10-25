const MS_PER_DAY = 24 * 60 * 60 * 1000;
const DAYS_PER_WEEK = 7;
const DAYS_PER_MONTH = 30;
const DAYS_PER_YEAR = 365;

function dayNumber(day: string): number {
  return Math.floor(Date.parse(`${day}T00:00:00Z`) / MS_PER_DAY);
}

// isFollowUpDue says whether a follow-up date (YYYY-MM-DD, or empty for none)
// is today or earlier. today is the owner's day in the same form.
export function isFollowUpDue(nextFollowUp: string, today: string): boolean {
  return nextFollowUp !== "" && nextFollowUp <= today;
}

// daysSince is how many whole days before today a YYYY-MM-DD date was, never
// below zero.
export function daysSince(day: string, today: string): number {
  return Math.max(0, dayNumber(today) - dayNumber(day));
}

// lastContactText says how long ago someone was last contacted, from a date.
export function lastContactText(lastContacted: string, today: string): string {
  if (lastContacted === "") {
    return "never";
  }
  const days = daysSince(lastContacted, today);
  if (days === 0) {
    return "today";
  }
  if (days < DAYS_PER_WEEK) {
    return `${days}d ago`;
  }
  if (days < DAYS_PER_MONTH) {
    return `${Math.floor(days / DAYS_PER_WEEK)}w ago`;
  }
  if (days < DAYS_PER_YEAR) {
    return `${Math.floor(days / DAYS_PER_MONTH)}mo ago`;
  }
  return `${Math.floor(days / DAYS_PER_YEAR)}y ago`;
}

const MS_PER_MINUTE = 60 * 1000;
const MINUTES_PER_HOUR = 60;
const HOURS_PER_DAY = 24;
const dayMonth = new Intl.DateTimeFormat("en-GB", { day: "numeric", month: "short", timeZone: "Asia/Kolkata" });

// eventTimeText says when a history entry happened: "just now", "5h ago",
// "3d ago", and a date such as "8 Oct" after a week.
export function eventTimeText(at: Date, now: Date): string {
  const minutes = Math.max(0, Math.floor((now.getTime() - at.getTime()) / MS_PER_MINUTE));
  if (minutes < 1) {
    return "just now";
  }
  if (minutes < MINUTES_PER_HOUR) {
    return `${minutes}m ago`;
  }
  const hours = Math.floor(minutes / MINUTES_PER_HOUR);
  if (hours < HOURS_PER_DAY) {
    return `${hours}h ago`;
  }
  const days = Math.floor(hours / HOURS_PER_DAY);
  if (days < DAYS_PER_WEEK) {
    return `${days}d ago`;
  }
  return dayMonth.format(at);
}
