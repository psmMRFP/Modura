export const locales = ["en", "zh-CN", "de", "fr", "es"] as const;
export type Locale = (typeof locales)[number];
export const languageNames: Record<Locale, string> = {
  en: "English",
  "zh-CN": "简体中文",
  de: "Deutsch",
  fr: "Français",
  es: "Español",
};
export function isLocale(value: string | undefined): value is Locale {
  return locales.some((locale) => locale === value);
}
interface Messages {
  language: string;
  eyebrow: string;
  title: string;
  intro: string;
  status: string;
  statusDetail: string;
  pillars: readonly { title: string; body: string }[];
  footer: string;
  notFound: string;
  home: string;
}
export const messages: Record<Locale, Messages> = {
  en: {
    language: "Language",
    eyebrow: "A clearer view of life abroad",
    title: "Where could you feel at home?",
    intro:
      "Visa, tax and living costs, grounded in sources you can check. WhereToLive is being built to help you make informed decisions about living abroad.",
    status: "Under development",
    statusDetail:
      "Search the published place catalogue. Visa, tax, costs and verified evidence are still being prepared.",
    pillars: [
      {
        title: "Check the evidence",
        body: "Published facts will include their sources, effective dates and verification history.",
      },
      {
        title: "Hear from residents",
        body: "Resident experiences will be shown separately from official facts, with residence verification.",
      },
      {
        title: "Choose what matters",
        body: "Personal fit will reflect your priorities, separately from data and resident scores.",
      },
    ],
    footer: "Sources. Context. Your decision.",
    notFound: "Page not found",
    home: "Back to home",
  },
  "zh-CN": {
    language: "语言",
    eyebrow: "让跨国生活决策更清晰",
    title: "哪里适合成为你的家？",
    intro:
      "把签证、税务与生活成本放在一起，用可核验的来源帮助你了解长期生活的选择。WhereToLive 正在建设中。",
    status: "正在开发",
    statusDetail:
      "当前可搜索已发布地点；签证、税务、成本及经核验证据仍在准备中。",
    pillars: [
      {
        title: "查看事实依据",
        body: "发布的事实将展示来源、生效时间和核验历史。",
      },
      {
        title: "了解居民体验",
        body: "经过居住验证的体验将独立呈现，与官方事实清楚区分。",
      },
      {
        title: "选择你的优先项",
        body: "个人适配度将根据你的偏好计算，与客观评分、居民评分分别展示。",
      },
    ],
    footer: "可查的来源，完整的语境，你自己的决定。",
    notFound: "页面不存在",
    home: "返回首页",
  },
  de: {
    language: "Sprache",
    eyebrow: "Klarer entscheiden über das Leben im Ausland",
    title: "Wo könntest du dich zu Hause fühlen?",
    intro:
      "Visa, Steuern und Lebenshaltungskosten mit überprüfbaren Quellen. WhereToLive entsteht, um fundierte Entscheidungen über das Leben im Ausland zu ermöglichen.",
    status: "In Entwicklung",
    statusDetail:
      "Der veröffentlichte Ortskatalog ist durchsuchbar. Visa, Steuern, Kosten und geprüfte Belege werden noch vorbereitet.",
    pillars: [
      {
        title: "Belege prüfen",
        body: "Veröffentlichte Fakten erhalten Quellen, Gültigkeitsdaten und einen Prüfverlauf.",
      },
      {
        title: "Erfahrungen kennenlernen",
        body: "Erfahrungen mit geprüftem Wohnaufenthalt werden getrennt von offiziellen Fakten dargestellt.",
      },
      {
        title: "Eigene Prioritäten setzen",
        body: "Die persönliche Eignung berücksichtigt deine Wünsche, getrennt von Daten- und Bewohnerbewertungen.",
      },
    ],
    footer: "Quellen. Kontext. Deine Entscheidung.",
    notFound: "Seite nicht gefunden",
    home: "Zur Startseite",
  },
  fr: {
    language: "Langue",
    eyebrow: "Mieux comprendre la vie à l’étranger",
    title: "Où pourriez-vous vous sentir chez vous ?",
    intro:
      "Visas, fiscalité et coût de la vie, avec des sources vérifiables. WhereToLive se construit pour éclairer vos décisions de vie à l’étranger.",
    status: "En développement",
    statusDetail:
      "Explorez le catalogue des lieux publiés. Visas, fiscalité, coûts et preuves vérifiées sont en préparation.",
    pillars: [
      {
        title: "Consulter les preuves",
        body: "Les faits publiés indiqueront leurs sources, leurs dates d’effet et leur historique de vérification.",
      },
      {
        title: "Écouter les résidents",
        body: "Les expériences de résidence vérifiées seront présentées séparément des faits officiels.",
      },
      {
        title: "Définir vos priorités",
        body: "L’adéquation personnelle reflétera vos préférences, indépendamment des scores de données et des résidents.",
      },
    ],
    footer: "Des sources. Du contexte. Votre décision.",
    notFound: "Page introuvable",
    home: "Retour à l’accueil",
  },
  es: {
    language: "Idioma",
    eyebrow: "Decide con más claridad sobre la vida en el extranjero",
    title: "¿Dónde podrías sentirte en casa?",
    intro:
      "Visados, impuestos y coste de vida, con fuentes verificables. WhereToLive está en desarrollo para ayudarte a decidir dónde vivir con información fiable.",
    status: "En desarrollo",
    statusDetail:
      "Busca en el catálogo de lugares publicados. Visados, impuestos, costes y pruebas verificadas están en preparación.",
    pillars: [
      {
        title: "Consulta las pruebas",
        body: "Los datos publicados incluirán fuentes, fechas de vigencia e historial de verificación.",
      },
      {
        title: "Conoce las experiencias",
        body: "Las experiencias con residencia verificada se mostrarán separadas de los datos oficiales.",
      },
      {
        title: "Elige tus prioridades",
        body: "La afinidad personal reflejará tus preferencias, separada de las puntuaciones de datos y residentes.",
      },
    ],
    footer: "Fuentes. Contexto. Tu decisión.",
    notFound: "Página no encontrada",
    home: "Volver al inicio",
  },
};
