import type { Locale } from "../../app/locales";
import type { SearchPublicPlacesParams } from "../../api/generated/places";

// URL state stays shareable; invalid input never causes unbounded requests.
export function searchInput(
  params: URLSearchParams,
  locale: Locale,
): SearchPublicPlacesParams | null {
  const q = params.get("q") ?? "";
  const offsetText = params.get("offset") ?? "0";
  const offset = Number(offsetText);
  if (
    [...q].length > 120 ||
    !/^\d+$/.test(offsetText) ||
    !Number.isSafeInteger(offset) ||
    offset > 10000
  )
    return null;
  return { q, locale, offset, limit: 20 };
}
export function placeSearchUrl(locale: Locale, q: string, offset = 0): string {
  const params = new URLSearchParams();
  if (q) params.set("q", q);
  if (offset) params.set("offset", String(offset));
  const query = params.toString();
  return `/${locale}/places${query ? `?${query}` : ""}`;
}
