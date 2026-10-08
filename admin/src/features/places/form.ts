import type { PlaceDetails } from "../../api/generated/modura";

export function normalizePlaceDetails(details: PlaceDetails): PlaceDetails {
  return {
    ...details,
    name: details.name.trim(),
    timezone: details.timezone?.trim() || null,
    currency: details.currency?.trim().toUpperCase() || null,
    latitude: details.latitude ?? null,
    longitude: details.longitude ?? null,
    languages: details.languages ?? [],
    aliases: (details.aliases ?? []).map((alias) => ({
      ...alias,
      name: alias.name.trim(),
      preferred: Boolean(alias.preferred),
    })),
  };
}
export const emptyPlaceDetails: PlaceDetails = {
  name: "",
  timezone: null,
  latitude: null,
  longitude: null,
  currency: null,
  languages: [],
  aliases: [],
};
