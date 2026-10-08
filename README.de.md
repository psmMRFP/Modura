<div align="center">

# WhereToLive

### Dein nächster Wohnort beginnt mit verlässlichen Informationen.

Eine offene Plattform für langfristiges Wohnen, Auswandern, Auslandsstudium und ortsunabhängige Arbeit.

[![CI](https://github.com/psmMRFP/WhereToLive/actions/workflows/ci.yml/badge.svg)](https://github.com/psmMRFP/WhereToLive/actions/workflows/ci.yml)
[![Lizenz](https://img.shields.io/badge/License-AGPL%203.0-blue.svg)](LICENSE)
[![Status](https://img.shields.io/badge/Stage-Early%20Development-orange.svg)](#aktueller-stand)

[English](README.md) · [简体中文](README.zh-CN.md) · **Deutsch** · [Français](README.fr.md) · [Español](README.es.md)

[Warum WhereToLive?](#warum-wheretolive) · [Aktueller Stand](#aktueller-stand) · [Lokale Entwicklung](#lokale-entwicklung) · [Mitwirken](#mitwirken)

</div>

---

## Warum WhereToLive?

Die Wahl eines Wohnorts erfordert mehr als einen Reiseführer. Visumsmöglichkeiten, Steuern, Mieten, Lebenshaltungskosten und die Erfahrungen tatsächlicher Bewohner sollten an einem Ort zugänglich sein.

WhereToLive verbindet **überprüfbare Fakten, Erfahrungen von Bewohnern und persönliche Präferenzen**, mit nachvollziehbaren Quellen, Gültigkeitsdaten und Änderungshistorien.

> Wie ein Ort objektiv abschneidet und wie gut er zu dir passt, sind zwei verschiedene Fragen.

| System             | Frage                                                 | Geplanter Maßstab                 |
| ------------------ | ----------------------------------------------------- | --------------------------------- |
| **Data Score**     | Wie schneidet der Ort anhand objektiver Daten ab?     | 0–100, nach einzelnen Dimensionen |
| **Resident Score** | Was sagen verifizierte Bewohner?                      | 1–10, mit bayesscher Schrumpfung  |
| **Your Fit**       | Passt der Ort zu Budget, Sprachbedarf und Lebensstil? | Nach deinen eigenen Gewichtungen  |

Diese drei Systeme bleiben unabhängig und sind noch nicht implementiert. **WhereToLive Atlas** ist der Projektcodename; **WhereToLive** ist der Produktname.

## Aktueller Stand

Das Projekt befindet sich in einer frühen Entwicklungsphase. Das öffentliche Ortsverzeichnis und die operative Grundlage sind lauffähig; ein produktiver Ortsdatensatz fehlt noch.

| Implementiert            | Funktionen                                                                                                                                                       |
| ------------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Öffentliche Website      | Ortssuche ohne Anmeldung, Seitennavigation und Detailseiten                                                                                                      |
| Mehrsprachige Oberfläche | English, 简体中文, Deutsch, Français, Español                                                                                                                    |
| Geografisches Modell     | Länder, Regionen, Städte, Stadtviertel, Inseln, stabile Slugs und mehrsprachige Namen                                                                            |
| Ortsverwaltung           | Entwürfe, Bearbeitung, Veröffentlichung, Rücknahme, Versionskonfliktschutz und transaktionales Audit                                                             |
| API-Vertrag              | Gemeinsames OpenAPI für Go und beide Frontends mit generierten Typen und Abfrageclients                                                                          |
| Kandidatenverwaltung | Kombinierte Filter für Land, Abdeckung und Veröffentlichung; Suche nach mehrsprachigen Namen |
| Orts-Testdaten | 40 manuell erstellte geografische Entwürfe nur für Entwicklung/E2E |
| Feedback-Kern | Privater manueller Eingang, Kategorien/Status, Ergebnisse, Versionsprüfung und Audit |
| Datenbankinitialisierung | Automatisches Anlegen einer fehlenden dedizierten Datenbank; Rolle und Datenbank `postgres` sind verboten                                                        |
| Verbraucherkonten        | Registrierung, E-Mail-Verifizierung, Anmeldung, Sitzungswiederherstellung und Kontowiederherstellung; bis zur vollständigen Deployment-Konfiguration deaktiviert |

**Als Nächstes:** Abdeckungs- und Prioritätsregeln sowie öffentliches Feedback mit Missbrauchsschutz → Quellen, Belege und Faktenversionen → Research Agent → Visa, Steuern und Lebenshaltungskosten → einheitliches Feedback → Wohnsitzverifizierung und Bewertungen → persönliche Eignung.

Bewertungen sollen per KI übersetzt werden, wenn ihre Originalsprache von der Lesesprache abweicht. Nutzer können automatische Übersetzung aktivieren und jederzeit das Original ansehen.

Informations- und Bewertungsdimensionen sollen im Adminbereich verwaltet werden, mit getrennten Schaltern für Anzeige und Wertung. Web3-/Kryptowährungsfreundlichkeit sowie Devisen- und Kapitalverkehrskontrollen sind vorgesehene Dimensionen. Diese Konfigurationsfunktion ist noch nicht implementiert.

## Produktprinzipien

- **Belege zuerst:** KI unterstützt Recherche, Extraktion, Übersetzung und Vergleich; sie ist selbst keine Informationsquelle.
- **Nachvollziehbare Historie:** zeitabhängige Fakten behalten Versionen, Quellen, Gültigkeitsdaten und Zeitpunkte der letzten Prüfung.
- **Getrennte Informationsströme:** offizielle Fakten, Erfahrungen der Community und aktuelle Warnungen werden klar unterschieden.
- **Datensparsamkeit:** Wohnsitznachweise bleiben privat, werden standardmäßig nicht an externe Sprachmodelle übermittelt und nach der Prüfung gelöscht; notwendige Verifizierungsmetadaten bleiben erhalten.
- **Kommerzielle Unabhängigkeit:** Werbung darf weder Abdeckungspriorität noch Data Score, Resident Score oder Your Fit beeinflussen.

Dies sind Designvorgaben; die zugehörigen Funktionen werden schrittweise umgesetzt.

## Technologie und Verzeichnisstruktur

**Modularer Go-Monolith + PostgreSQL + React.** Fachmodule kommunizieren lokal. Die Suche nutzt zunächst PostgreSQL, ohne separaten Suchcluster oder Microservice-Architektur.

```text
WhereToLive/
├── backend/     Go-API, Fachmodule, SQL-Abfragen und Migrationen
├── web/         Öffentliche React-Website
├── admin/       React-Oberfläche für Betrieb und Moderation
├── api/         Verbindlicher HTTP-Vertrag und Generierungskonfiguration
├── scripts/     Prüfungen für Vertrag, Zuständigkeiten und Quellcodegrenzen
└── .github/     CI-Workflows
```

Die Frontends verwenden React, Vite, React Router, TanStack Query und Ant Design. SQL-Abfragen werden mit sqlc erstellt; HTTP-Typen und Clients werden aus OpenAPI generiert.

## Lokale Entwicklung

### 1. Umgebung vorbereiten

Maßgeblich ist die Repository-Konfiguration: derzeit **Go 1.27, Node.js ≥ 26 und npm ≥ 12**. Für die vollständige Prüfung müssen außerdem `oapi-codegen`, `sqlc`, `golangci-lint`, Python 3 und Make installiert sein. Festgelegte Toolversionen stehen in der [CI-Konfiguration](.github/workflows/ci.yml); CI verwendet PostgreSQL 17.

Frontend-Abhängigkeiten aus dem Repository-Hauptverzeichnis installieren:

```fish
npm ci --prefix admin
npm ci --prefix web
```

### 2. Backend konfigurieren

[backend/.env.example](backend/.env.example) dient als Referenz. Die Konfiguration erfolgt über die Prozessumgebung oder die Geheimnisverwaltung der Deployment-Umgebung. `.env`-Dateien werden nicht automatisch geladen.

| Variable                      | Zweck                                                                                     |
| ----------------------------- | ----------------------------------------------------------------------------------------- |
| `WHERETOLIVE_DATABASE_URL`         | Verbindungs-URL für eine dedizierte PostgreSQL-Rolle und benannte Datenbank; erforderlich |
| `WHERETOLIVE_AUTH_SIGNING_KEY`     | Signaturschlüssel mit mindestens 32 Bytes; erforderlich                                   |
| `WHERETOLIVE_AUTH_COOKIE_SECURE`   | Für lokale HTTP-Entwicklung `false`; produktiv `true` mit TLS                             |
| `WHERETOLIVE_DATABASE_AUTO_CREATE` | Standard `true`; nach Bereitstellung auf `false` umstellbar                               |

Die Datenbank wird nur angelegt, wenn PostgreSQL ausdrücklich meldet, dass das Ziel nicht existiert. Die Verbindung erfolgt über `template1`, die Erstellung aus `template0`; die dedizierte Rolle benötigt `CREATEDB`. **Das Anlegen der Datenbank führt keine Schemamigrationen aus.**

Leere Datenbank ohne Konfiguration des Signaturschlüssels initialisieren:

```fish
cd backend
go run ./cmd/wheretolive-db-init
```

Danach die [Migrationen](backend/internal/platform/database/migrations) mit einem zu `golang-migrate` kompatiblen Werkzeug in Reihenfolge anwenden. API aus `backend/` starten:

```fish
go run ./cmd/wheretolive
```

### 3. Frontends starten

Jeweils in einem eigenen Terminal im Repository-Hauptverzeichnis ausführen:

```fish
npm run dev --prefix web
```

```fish
npm run dev --prefix admin
```

| Dienst              | Lokale Adresse          |
| ------------------- | ----------------------- |
| Öffentliche Website | `http://localhost:5174` |
| Adminoberfläche     | `http://localhost:5173` |
| Backend-API         | `http://localhost:8080` |

Die Entwicklungsserver leiten `/api` an das Backend weiter. Ortsseiten verwenden `/{locale}/places/{slug}`; ein Sprachwechsel erhält den Slug. Ohne veröffentlichte Orte erscheint ein leeres Verzeichnis. Die öffentliche Website ist derzeit auf `noindex` gesetzt.

## Prüfung

Im Repository-Hauptverzeichnis:

```fish
make verify
```

Geprüft werden Generierungskonsistenz, OpenAPI, Go-Formatierung und statische Analyse, Unit-Tests, Formatierung / Lint / Typen / Komponententests / Builds beider Frontends sowie Tabellenzuständigkeiten und Quellcodegrenzen.

PostgreSQL-Integrationstests benötigen `WHERETOLIVE_TEST_DATABASE_URL` für eine dedizierte Datenbank, deren Name auf `_test` endet. Sie setzen deren `wheretolive`-Schema zurück. Niemals eine Geschäftsdatenbank verwenden.

```fish
make backend-test-integration
```

Browserprüfungen der öffentlichen Website verwenden installiertes Chromium und feste API-Testdaten:

```fish
make web-e2e
```

Sie prüfen die Seiteninteraktion und ersetzen keine Datenbanktests. Admin-E2E läuft mit `make admin-e2e` und benötigt eine Datenbank namens `wheretolive_test`. `make verify-release` prüft zusätzlich Abhängigkeiten auf Sicherheitslücken und Lizenzen.

## Mitwirken

Zuerst [AGENTS.md](AGENTS.md), bestehende Module und den [OpenAPI-Vertrag](api/openapi.yaml) lesen. HTTP-Vertrag, Implementierung, generierte Clients und Tests gemeinsam aktualisieren. Generierte Dateien nicht manuell bearbeiten.

Arbeitsdokumente zu Architektur und Produkt liegen derzeit außerhalb des Repositorys; historische Dokumentationslinks können fehlen. Keine lokalen Geheimnisse, Umgebungskonfigurationen, Wohnsitznachweise oder privaten Nutzerdaten committen.

Probleme und Vorschläge sind in den [Issues](https://github.com/psmMRFP/WhereToLive/issues) willkommen, ebenso Pull Requests. Bei Änderungen an Projektstatus oder Entwicklungsanweisungen alle fünf README-Versionen synchron halten.

## Lizenz

[GNU AGPL v3.0](LICENSE) (`AGPL-3.0-only`). Abhängigkeiten und Datenquellen behalten ihre jeweiligen Lizenzen.
