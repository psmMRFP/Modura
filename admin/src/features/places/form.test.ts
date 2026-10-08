import { describe, expect, it } from "vitest";
import { emptyPlaceDetails, normalizePlaceDetails } from "./form";
describe("place metadata form", () => {
  it("preserves explicit absence and normalizes fields before submission", () => {
    expect(
      normalizePlaceDetails({
        ...emptyPlaceDetails,
        name: " Munich ",
        currency: " eur ",
        timezone: " ",
        latitude: 0,
        longitude: 0,
        aliases: [{ locale: "de", name: " München ", preferred: true }],
      }),
    ).toEqual({
      ...emptyPlaceDetails,
      name: "Munich",
      currency: "EUR",
      timezone: null,
      latitude: 0,
      longitude: 0,
      aliases: [{ locale: "de", name: "München", preferred: true }],
    });
  });
  it("does not mutate loaded metadata or confuse zero coordinates with missing ones", () => {
    const original = {
      ...emptyPlaceDetails,
      name: "Munich",
      aliases: [{ locale: "de", name: " München ", preferred: false }],
    };
    normalizePlaceDetails(original);
    expect(original.aliases[0].name).toBe(" München ");
    expect(normalizePlaceDetails(emptyPlaceDetails).latitude).toBeNull();
  });
});
