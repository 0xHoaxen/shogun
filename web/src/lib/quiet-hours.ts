const MINUTES_PER_HOUR = 60;
const TIME_PATTERN = /^([01]\d|2[0-3]):([0-5]\d)$/;

// The window offered when the owner first turns quiet hours on.
export const DEFAULT_QUIET_FROM = "22:00";
export const DEFAULT_QUIET_TO = "08:00";

function twoDigits(n: number): string {
  return String(n).padStart(2, "0");
}

// minutesToTime turns minutes after midnight into the "HH:MM" of a time input.
export function minutesToTime(minutes: number): string {
  return `${twoDigits(Math.floor(minutes / MINUTES_PER_HOUR))}:${twoDigits(minutes % MINUTES_PER_HOUR)}`;
}

// timeToMinutes turns the "HH:MM" of a time input into minutes after midnight,
// or null when it is empty or not a time of day.
export function timeToMinutes(value: string): number | null {
  const match = TIME_PATTERN.exec(value);
  return match ? Number(match[1]) * MINUTES_PER_HOUR + Number(match[2]) : null;
}
