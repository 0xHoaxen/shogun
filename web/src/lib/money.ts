const MICROS_PER_DOLLAR = 1_000_000;
const MICROS_PER_DOLLAR_BIG = BigInt(MICROS_PER_DOLLAR);
const MAX_MICRO_DIGITS = 6;

// Dollars are shown with at least cents and at most four decimals, so a spend
// of $0.0040 does not read as $0.00.
const dollars = new Intl.NumberFormat("en-US", {
  style: "currency",
  currency: "USD",
  minimumFractionDigits: 2,
  maximumFractionDigits: 4,
});

// formatMicros renders micro-dollars, for example 4000000n as "$4.00".
export function formatMicros(micros: bigint): string {
  return dollars.format(Number(micros) / MICROS_PER_DOLLAR);
}

const DOLLAR_AMOUNT = new RegExp(`^\\d+(\\.\\d{1,${MAX_MICRO_DIGITS}})?$`);

// parseDollars reads an amount such as "12.5" into micro-dollars. It returns
// null for anything else, including negative numbers and more than six
// decimals.
export function parseDollars(text: string): bigint | null {
  const trimmed = text.trim();
  if (!DOLLAR_AMOUNT.test(trimmed)) {
    return null;
  }
  const [whole, fraction = ""] = trimmed.split(".");
  const micros = fraction.padEnd(MAX_MICRO_DIGITS, "0");
  return BigInt(whole) * MICROS_PER_DOLLAR_BIG + BigInt(micros);
}

// dollarsInput renders micro-dollars the way parseDollars reads them back, for
// an edit field: "20" for twenty dollars, "0.5" for fifty cents.
export function dollarsInput(micros: bigint): string {
  const whole = micros / MICROS_PER_DOLLAR_BIG;
  const fraction = (micros % MICROS_PER_DOLLAR_BIG).toString().padStart(MAX_MICRO_DIGITS, "0").replace(/0+$/, "");
  return fraction === "" ? whole.toString() : `${whole}.${fraction}`;
}
