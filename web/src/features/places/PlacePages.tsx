import { useEffect } from "react";
import { Form, Link, useParams, useSearchParams } from "react-router-dom";
import {
  useGetPublicPlace,
  useSearchPublicPlaces,
} from "../../api/generated/places";
import { isLocale, type Locale } from "../../app/locales";
import { PlaceCard } from "./PlaceCard";
import { placeMessages } from "./messages";
import { placeSearchUrl, searchInput } from "./search";

function useLocale(): Locale {
  const { locale } = useParams();
  return isLocale(locale) ? locale : "en";
}
export function SearchForm({
  locale,
  initialValue = "",
}: {
  locale: Locale;
  initialValue?: string;
}) {
  const copy = placeMessages[locale];
  return (
    <Form method="get" action={`/${locale}/places`} className="search-form">
      <label htmlFor="place-search">{copy.search}</label>
      <div>
        <input
          id="place-search"
          name="q"
          type="search"
          maxLength={120}
          placeholder={copy.placeholder}
          defaultValue={initialValue}
          key={initialValue}
        />
        <button type="submit">{copy.search}</button>
      </div>
    </Form>
  );
}
function QueryStatus({
  locale,
  pending,
  failed,
  retry,
}: {
  locale: Locale;
  pending: boolean;
  failed: boolean;
  retry: () => void;
}) {
  const copy = placeMessages[locale];
  if (pending) return <p role="status">{copy.loading}</p>;
  if (failed)
    return (
      <div role="alert">
        <p>{copy.unavailable}</p>
        <button onClick={retry}>{copy.retry}</button>
      </div>
    );
  return null;
}
export function PlacesPage() {
  const locale = useLocale();
  const copy = placeMessages[locale];
  const [params] = useSearchParams();
  const input = searchInput(params, locale);
  const query = useSearchPublicPlaces(input ?? undefined, {
    query: { enabled: input !== null, retry: false, staleTime: 60000 },
  });
  useEffect(() => {
    document.title = `${copy.browse} · WhereToLive`;
  }, [copy.browse]);
  const page = query.data?.status === 200 ? query.data.data : undefined;
  return (
    <section className="catalogue">
      <h1>{copy.browse}</h1>
      <SearchForm locale={locale} initialValue={params.get("q") ?? ""} />
      {!input ? (
        <p role="alert">{copy.invalid}</p>
      ) : (
        <>
          <QueryStatus
            locale={locale}
            pending={query.isPending}
            failed={
              query.isError || (!!query.data && query.data.status !== 200)
            }
            retry={() => void query.refetch()}
          />
          {page && (
            <>
              {page.items.length === 0 ? (
                <p role="status">{copy.empty}</p>
              ) : (
                <div className="place-grid">
                  {page.items.map((place) => (
                    <PlaceCard key={place.id} place={place} locale={locale} />
                  ))}
                </div>
              )}
              <nav className="pagination" aria-label={copy.browse}>
                {(input.offset ?? 0) > 0 && (
                  <Link
                    to={placeSearchUrl(
                      locale,
                      input.q ?? "",
                      Math.max(0, (input.offset ?? 0) - 20),
                    )}
                  >
                    {copy.previous}
                  </Link>
                )}
                {page.nextOffset !== null && page.nextOffset <= 10000 && (
                  <Link
                    to={placeSearchUrl(locale, input.q ?? "", page.nextOffset)}
                  >
                    {copy.next}
                  </Link>
                )}
              </nav>
            </>
          )}
        </>
      )}
    </section>
  );
}
export function PlacePage() {
  const locale = useLocale();
  const copy = placeMessages[locale];
  const { slug = "" } = useParams();
  const query = useGetPublicPlace(
    slug,
    { locale },
    { query: { retry: false, staleTime: 60000 } },
  );
  const place = query.data?.status === 200 ? query.data.data : undefined;
  useEffect(() => {
    document.title = `${place?.displayName ?? copy.browse} · WhereToLive`;
  }, [place?.displayName, copy.browse]);
  return (
    <section className="place-detail">
      <Link to={`/${locale}/places`}>{copy.back}</Link>
      {query.data?.status === 404 ? (
        <h1>{copy.notFound}</h1>
      ) : (
        <QueryStatus
          locale={locale}
          pending={query.isPending}
          failed={query.isError || (!!query.data && query.data.status !== 200)}
          retry={() => void query.refetch()}
        />
      )}
      {place && (
        <>
          <p className="eyebrow">{copy.types[place.type]}</p>
          <h1>{place.displayName}</h1>
          {place.name !== place.displayName && (
            <p className="intro">{place.name}</p>
          )}
          <dl className="place-facts">
            <div>
              <dt>{copy.country}</dt>
              <dd>{place.countryCode}</dd>
            </div>
            <div>
              <dt>{copy.timezone}</dt>
              <dd>{place.timezone ?? copy.unknown}</dd>
            </div>
            <div>
              <dt>{copy.currency}</dt>
              <dd>{place.currency ?? copy.unknown}</dd>
            </div>
            <div>
              <dt>{copy.languages}</dt>
              <dd>
                {place.languages.length
                  ? place.languages.join(", ")
                  : copy.unknown}
              </dd>
            </div>
            <div>
              <dt>{copy.coverage}</dt>
              <dd>{copy.levels[place.coverageLevel]}</dd>
            </div>
            <div>
              <dt>{copy.published}</dt>
              <dd>
                <time dateTime={place.publishedAt}>
                  {new Intl.DateTimeFormat(locale, {
                    dateStyle: "medium",
                    timeZone: "UTC",
                  }).format(new Date(place.publishedAt))}
                </time>
              </dd>
            </div>
          </dl>
          <p className="metadata-note">{copy.publicationNote}</p>
          <aside className="status">{copy.incomplete}</aside>
        </>
      )}
    </section>
  );
}
