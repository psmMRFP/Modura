import { useEffect } from "react";
import { Link, Outlet, useLocation, useParams } from "react-router-dom";
import { SearchForm } from "../features/places/PlacePages";
import { placeMessages } from "../features/places/messages";
import { isLocale, languageNames, locales, messages } from "./locales";

export function LocaleLayout() {
  const { locale: requested } = useParams();
  const location = useLocation();
  const locale = isLocale(requested) ? requested : "en";
  const copy = messages[locale];
  useEffect(() => {
    document.documentElement.lang = locale;
  }, [locale]);
  if (!isLocale(requested)) return <NotFound />;
  const suffix = location.pathname.slice(`/${locale}`.length);
  return (
    <div className="page">
      <header>
        <Link className="brand" to={`/${locale}`}>
          WhereToLive<span aria-hidden="true">↗</span>
        </Link>
        <nav aria-label={copy.language}>
          {locales.map((value) => (
            <Link
              key={value}
              to={`/${value}${suffix}${location.search}`}
              lang={value}
              hrefLang={value}
              aria-current={value === locale ? "page" : undefined}
            >
              {languageNames[value]}
            </Link>
          ))}
        </nav>
      </header>
      <main>
        <Outlet />
      </main>
      <footer>
        <span>WhereToLive</span>
        <p>{copy.footer}</p>
      </footer>
    </div>
  );
}
export function Home() {
  const { locale: requested } = useParams();
  const locale = isLocale(requested) ? requested : "en";
  const copy = messages[locale];
  useEffect(() => {
    document.title = `WhereToLive · ${copy.eyebrow}`;
  }, [copy.eyebrow]);
  return (
    <>
      <section className="hero" aria-labelledby="title">
        <p className="eyebrow">{copy.eyebrow}</p>
        <h1 id="title">{copy.title}</h1>
        <p className="intro">{copy.intro}</p>
        <SearchForm locale={locale} />
        <Link className="browse-link" to={`/${locale}/places`}>
          {placeMessages[locale].browse} ↗
        </Link>
        <aside className="status">
          <strong>{copy.status}</strong>
          <p>{copy.statusDetail}</p>
        </aside>
      </section>
      <section className="pillars">
        {copy.pillars.map((pillar, index) => (
          <article key={pillar.title}>
            <span className="number" aria-hidden="true">
              0{index + 1}
            </span>
            <h2>{pillar.title}</h2>
            <p>{pillar.body}</p>
          </article>
        ))}
      </section>
    </>
  );
}
export function NotFound() {
  const { locale } = useParams();
  const selected = isLocale(locale) ? locale : "en";
  const copy = messages[selected];
  useEffect(() => {
    document.documentElement.lang = selected;
    document.title = `WhereToLive · ${copy.notFound}`;
  }, [selected, copy.notFound]);
  return (
    <section className="missing">
      <h1>{copy.notFound}</h1>
      <Link to={`/${selected}`}>{copy.home}</Link>
    </section>
  );
}
