import type { Amount } from "./model";

function localized(amount: string, options: Intl.NumberFormatOptions) {
  const { format } = new Intl.NumberFormat("en-US", options);
  // ECMA-402 accepts decimal strings. TS 5.9 still omits them from the format overload.
  return (format as unknown as (decimal: string) => string)(amount);
}

function decimals(digits: number) {
  return { minimumFractionDigits: digits, maximumFractionDigits: digits };
}

export function money(amount: Amount, digits = 2) {
  return amount === null ? "—" : localized(amount, decimals(digits));
}

export function usd(amount: Amount) {
  return amount === null ? "—" : "$" + money(amount);
}

export function signed(amount: Amount, digits = 2) {
  if (amount === null) return "—";
  const sign = numberClass(amount);
  return (
    (sign === "negative" ? "−" : sign === "positive" ? "+" : "") +
    localized(amount.replace(/^-/, ""), decimals(digits))
  );
}

export function percent(amount: Amount, digits = 2, sign = true) {
  if (amount === null) return "—";
  const prefix = sign && numberClass(amount) === "positive" ? "+" : "";
  return prefix + localized(amount, { style: "percent", ...decimals(digits) });
}

export function numberClass(amount: Amount) {
  if (amount === null || !/[1-9]/.test(amount)) return "";
  return amount.startsWith("-") ? "negative" : "positive";
}
