const OWNER_TIME_ZONE = "Asia/Kolkata";

// The "en-CA" locale formats a date as YYYY-MM-DD.
const dayFormat = new Intl.DateTimeFormat("en-CA", { timeZone: OWNER_TIME_ZONE });

// ownerToday is the calendar day at now in the owner's zone, as YYYY-MM-DD.
export function ownerToday(now: Date): string {
  return dayFormat.format(now);
}

// monthStart is the first day of the month of day, a YYYY-MM-DD date.
export function monthStart(day: string): string {
  return `${day.slice(0, 7)}-01`;
}
