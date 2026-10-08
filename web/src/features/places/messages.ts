import type { Locale } from "../../app/locales";

interface Copy {
  search: string;
  placeholder: string;
  browse: string;
  loading: string;
  unavailable: string;
  retry: string;
  empty: string;
  back: string;
  next: string;
  previous: string;
  invalid: string;
  notFound: string;
  country: string;
  timezone: string;
  currency: string;
  languages: string;
  unknown: string;
  coverage: string;
  levels: readonly string[];
  published: string;
  publicationNote: string;
  incomplete: string;
  types: Record<string, string>;
}
export const placeMessages: Record<Locale, Copy> = {
  en: {
    search: "Find a place",
    placeholder: "City, region or country",
    browse: "Browse places",
    loading: "Loading places…",
    unavailable: "We couldn’t load the catalogue. Please try again.",
    retry: "Try again",
    empty: "No published places match this search.",
    back: "Back to places",
    next: "Next page",
    previous: "Previous page",
    invalid: "Please check the search parameters.",
    notFound: "This place is not available.",
    country: "Country code",
    timezone: "Time zone",
    currency: "Currency",
    languages: "Languages",
    unknown: "Not yet available",
    coverage: "Coverage",
    levels: ["Candidate", "Basic", "Relocation", "Full"],
    published: "Catalogue published",
    publicationNote:
      "This is the publication date of the place entry, not the verification date of individual facts.",
    incomplete:
      "Visa, tax, costs and evidence are still being prepared. No scores are available yet.",
    types: {
      country: "Country",
      region: "Region",
      island: "Island",
      city: "City",
      district: "District",
    },
  },
  "zh-CN": {
    search: "搜索地点",
    placeholder: "城市、地区或国家",
    browse: "浏览地点",
    loading: "正在加载地点…",
    unavailable: "暂时无法加载地点目录，请稍后重试。",
    retry: "重试",
    empty: "没有找到匹配的已发布地点。",
    back: "返回地点列表",
    next: "下一页",
    previous: "上一页",
    invalid: "请检查搜索参数。",
    notFound: "此地点暂不可用。",
    country: "国家代码",
    timezone: "时区",
    currency: "货币",
    languages: "语言",
    unknown: "暂无数据",
    coverage: "覆盖程度",
    levels: ["候选", "基础", "迁居", "完整"],
    published: "地点目录发布时间",
    publicationNote: "这是地点条目的发布时间，不代表各项事实的核验时间。",
    incomplete: "签证、税务、成本和证据内容仍在准备中，目前暂无评分。",
    types: {
      country: "国家",
      region: "地区",
      island: "岛屿",
      city: "城市",
      district: "城区",
    },
  },
  de: {
    search: "Ort suchen",
    placeholder: "Stadt, Region oder Land",
    browse: "Orte entdecken",
    loading: "Orte werden geladen…",
    unavailable:
      "Der Katalog konnte nicht geladen werden. Bitte erneut versuchen.",
    retry: "Erneut versuchen",
    empty: "Keine veröffentlichten Orte für diese Suche.",
    back: "Zur Ortsliste",
    next: "Nächste Seite",
    previous: "Vorherige Seite",
    invalid: "Bitte die Suchparameter prüfen.",
    notFound: "Dieser Ort ist nicht verfügbar.",
    country: "Ländercode",
    timezone: "Zeitzone",
    currency: "Währung",
    languages: "Sprachen",
    unknown: "Noch nicht verfügbar",
    coverage: "Abdeckung",
    levels: ["Kandidat", "Basis", "Umzug", "Vollständig"],
    published: "Katalogeintrag veröffentlicht",
    publicationNote:
      "Dies ist das Veröffentlichungsdatum des Ortseintrags, nicht das Prüfdatum einzelner Fakten.",
    incomplete:
      "Visa, Steuern, Kosten und Belege werden noch vorbereitet. Bewertungen sind noch nicht verfügbar.",
    types: {
      country: "Land",
      region: "Region",
      island: "Insel",
      city: "Stadt",
      district: "Stadtteil",
    },
  },
  fr: {
    search: "Chercher un lieu",
    placeholder: "Ville, région ou pays",
    browse: "Explorer les lieux",
    loading: "Chargement des lieux…",
    unavailable: "Impossible de charger le catalogue. Veuillez réessayer.",
    retry: "Réessayer",
    empty: "Aucun lieu publié ne correspond à cette recherche.",
    back: "Retour aux lieux",
    next: "Page suivante",
    previous: "Page précédente",
    invalid: "Veuillez vérifier les paramètres de recherche.",
    notFound: "Ce lieu n’est pas disponible.",
    country: "Code pays",
    timezone: "Fuseau horaire",
    currency: "Devise",
    languages: "Langues",
    unknown: "Pas encore disponible",
    coverage: "Couverture",
    levels: ["Candidat", "Base", "Installation", "Complète"],
    published: "Publication dans le catalogue",
    publicationNote:
      "Il s’agit de la date de publication du lieu, et non de la vérification de chaque fait.",
    incomplete:
      "Visas, fiscalité, coûts et preuves sont en préparation. Aucun score n’est encore disponible.",
    types: {
      country: "Pays",
      region: "Région",
      island: "Île",
      city: "Ville",
      district: "Quartier",
    },
  },
  es: {
    search: "Buscar un lugar",
    placeholder: "Ciudad, región o país",
    browse: "Explorar lugares",
    loading: "Cargando lugares…",
    unavailable: "No pudimos cargar el catálogo. Inténtalo de nuevo.",
    retry: "Reintentar",
    empty: "No hay lugares publicados que coincidan con la búsqueda.",
    back: "Volver a los lugares",
    next: "Página siguiente",
    previous: "Página anterior",
    invalid: "Revisa los parámetros de búsqueda.",
    notFound: "Este lugar no está disponible.",
    country: "Código de país",
    timezone: "Zona horaria",
    currency: "Moneda",
    languages: "Idiomas",
    unknown: "Aún no disponible",
    coverage: "Cobertura",
    levels: ["Candidato", "Básica", "Mudanza", "Completa"],
    published: "Publicación en el catálogo",
    publicationNote:
      "Esta es la fecha de publicación del lugar, no la fecha de verificación de cada dato.",
    incomplete:
      "Visados, impuestos, costes y pruebas están en preparación. Aún no hay puntuaciones.",
    types: {
      country: "País",
      region: "Región",
      island: "Isla",
      city: "Ciudad",
      district: "Barrio",
    },
  },
};
