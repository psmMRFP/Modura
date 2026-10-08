import { Link } from "react-router-dom";
import type { PublicPlace } from "../../api/generated/places";
import type { Locale } from "../../app/locales";
import { placeMessages } from "./messages";

export function PlaceCard({
  place,
  locale,
}: {
  place: PublicPlace;
  locale: Locale;
}) {
  const copy = placeMessages[locale];
  return (
    <article className="place-card">
      <p className="eyebrow">
        {copy.types[place.type]} · {place.countryCode}
      </p>
      <h2>
        <Link to={`/${locale}/places/${place.slug}`}>{place.displayName}</Link>
      </h2>
      {place.displayName !== place.name && <p>{place.name}</p>}
      <p>
        {copy.coverage}: {copy.levels[place.coverageLevel]}
      </p>
    </article>
  );
}
