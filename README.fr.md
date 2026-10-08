<div align="center">

# WhereToLive

### Choisir où vivre commence par des informations fiables.

Une plateforme ouverte pour vivre durablement ailleurs, s'expatrier, étudier à l'étranger et travailler à distance.

[![CI](https://github.com/psmMRFP/WhereToLive/actions/workflows/ci.yml/badge.svg)](https://github.com/psmMRFP/WhereToLive/actions/workflows/ci.yml)
[![Licence](https://img.shields.io/badge/License-AGPL%203.0-blue.svg)](LICENSE)
[![État](https://img.shields.io/badge/Stage-Early%20Development-orange.svg)](#état-actuel)

[English](README.md) · [简体中文](README.zh-CN.md) · [Deutsch](README.de.md) · **Français** · [Español](README.es.md)

[Pourquoi WhereToLive ?](#pourquoi-wheretolive-) · [État actuel](#état-actuel) · [Développement local](#développement-local) · [Contribuer](#contribuer)

</div>

---

## Pourquoi WhereToLive ?

Choisir un lieu de vie demande plus qu'un guide touristique. Les possibilités de visa, la fiscalité, les loyers, le coût du quotidien et l'expérience des personnes qui y ont vécu devraient être accessibles au même endroit.

WhereToLive réunit **des faits vérifiables, des expériences de résidents et des préférences personnelles**, avec des sources explicites, des dates d'application et un historique des changements.

> La qualité objective d'un lieu et son adéquation à vos besoins sont deux questions différentes.

| Système            | Question                                                                               | Échelle prévue                       |
| ------------------ | -------------------------------------------------------------------------------------- | ------------------------------------ |
| **Data Score**     | Quels sont les résultats selon des critères objectifs ?                                | 0–100, avec le détail par dimension  |
| **Resident Score** | Qu'en pensent les résidents vérifiés ?                                                 | 1–10, avec régularisation bayésienne |
| **Your Fit**       | Le lieu correspond-il à votre budget, vos besoins linguistiques et votre mode de vie ? | Selon vos propres pondérations       |

Ces trois systèmes restent indépendants et ne sont pas encore implémentés. **Modura Atlas** est le nom de code de la transformation ; **WhereToLive** est le nom du produit.

## État actuel

Le projet est au début de son développement et s'appuie sur Modura. Le catalogue public des lieux et les bases de l'administration fonctionnent ; aucun jeu de données de production sur les lieux n'est encore disponible.

| Implémenté                | Fonctionnalités                                                                                                   |
| ------------------------- | ----------------------------------------------------------------------------------------------------------------- |
| Site public               | Recherche de lieux sans compte, pagination et pages détaillées                                                    |
| Interface multilingue     | English, 简体中文, Deutsch, Français, Español                                                                     |
| Modèle géographique       | Pays, régions, villes, quartiers, îles, slugs stables et alias multilingues                                       |
| Administration des lieux  | Brouillons, modification, publication, retrait, protection contre les conflits de version et audit transactionnel |
| Contrat API               | OpenAPI commun à Go et aux deux interfaces, avec types et clients de requêtes générés                             |
| Initialisation de la base | Création automatique d'une base dédiée absente ; rôle et base par défaut `postgres` interdits                     |

**Prochaines étapes :** inscription publique et vérification de l'adresse e-mail → sources, preuves et versions des faits → Research Agent → visas, fiscalité et coût de la vie → retours unifiés → vérification de résidence et avis → adéquation personnelle.

La traduction des avis par IA est prévue lorsque la langue de lecture diffère de celle de l'avis original. Les utilisateurs pourront activer la traduction automatique et toujours consulter l'original.

Les dimensions d'information et de notation seront administrables, avec des interrupteurs distincts pour l'affichage et le calcul des scores. L'environnement Web3 / cryptomonnaies et les contrôles des changes / capitaux font partie des dimensions envisagées. Cette gestion n'est pas encore implémentée.

## Principes du produit

- **Les preuves d'abord :** l'IA aide à rechercher, extraire, traduire et comparer ; elle n'est pas une source d'information.
- **Un historique traçable :** les faits sensibles au temps conservent leurs versions, sources, dates d'application et dates de vérification.
- **Des informations distinctes :** faits officiels, expériences de la communauté et alertes actuelles sont clairement séparés.
- **Minimisation des données personnelles :** les justificatifs de résidence restent privés, ne sont pas transmis par défaut à des modèles tiers et sont supprimés après examen ; seules les métadonnées nécessaires sont conservées.
- **Indépendance commerciale :** la publicité ne peut influencer la priorité de couverture, Data Score, Resident Score ou Your Fit.

Il s'agit d'engagements de conception ; les fonctionnalités correspondantes sont développées progressivement.

## Technologie et structure

**Monolithe modulaire Go + PostgreSQL + React.** Les modules métier communiquent localement. La recherche utilise d'abord PostgreSQL, sans cluster de recherche séparé ni architecture de microservices.

```text
WhereToLive/
├── backend/     API Go, modules métier, requêtes SQL et migrations
├── web/         Site public React
├── admin/       Interface React d'administration et de modération
├── api/         Contrat HTTP de référence et configuration de génération
├── scripts/     Vérifications des contrats, responsabilités et frontières du code
└── .github/     Workflows CI
```

Les interfaces utilisent React, Vite, React Router, TanStack Query et Ant Design. Les requêtes SQL utilisent sqlc ; les types HTTP et les clients sont générés depuis OpenAPI.

## Développement local

### 1. Préparer l'environnement

La configuration du dépôt fait foi : actuellement **Go 1.27, Node.js ≥ 26 et npm ≥ 12**. La vérification complète nécessite aussi `oapi-codegen`, `sqlc`, `golangci-lint`, Python 3 et Make déjà installés. Les versions des outils figurent dans la [configuration CI](.github/workflows/ci.yml), qui utilise PostgreSQL 17.

Installer les dépendances frontend verrouillées depuis la racine du dépôt :

```fish
npm ci --prefix admin
npm ci --prefix web
```

### 2. Configurer le backend

Consulter [backend/.env.example](backend/.env.example). Fournir la configuration via l'environnement du processus ou le mécanisme de gestion des secrets du déploiement. Les fichiers `.env` ne sont pas chargés automatiquement.

| Variable                      | Rôle                                                                     |
| ----------------------------- | ------------------------------------------------------------------------ |
| `MODURA_DATABASE_URL`         | URL de connexion avec rôle PostgreSQL dédié et base nommée ; obligatoire |
| `MODURA_AUTH_SIGNING_KEY`     | Clé de signature d'au moins 32 octets ; obligatoire                      |
| `MODURA_AUTH_COOKIE_SECURE`   | `false` pour le développement HTTP local ; `true` avec TLS en production |
| `MODURA_DATABASE_AUTO_CREATE` | `true` par défaut ; peut être désactivé après provisionnement            |

La création n'est tentée que si PostgreSQL indique explicitement que la base cible n'existe pas. La connexion passe par `template1` et la création utilise `template0` ; le rôle dédié doit disposer de `CREATEDB`. **Créer la base n'applique pas les migrations du schéma.**

Initialiser une base vide sans configurer la clé de signature :

```fish
cd backend
go run ./cmd/modura-db-init
```

Appliquer ensuite les [migrations](backend/internal/platform/database/migrations) dans l'ordre avec un outil compatible `golang-migrate`. Démarrer l'API depuis `backend/` :

```fish
go run ./cmd/modura
```

### 3. Démarrer les interfaces

Exécuter chaque commande dans un terminal distinct à la racine du dépôt :

```fish
npm run dev --prefix web
```

```fish
npm run dev --prefix admin
```

| Service        | Adresse locale          |
| -------------- | ----------------------- |
| Site public    | `http://localhost:5174` |
| Administration | `http://localhost:5173` |
| API backend    | `http://localhost:8080` |

Les serveurs de développement transmettent `/api` au backend. Les pages des lieux utilisent `/{locale}/places/{slug}` ; changer de langue conserve le slug. Sans lieu publié, le catalogue reste vide. Le site public conserve actuellement la directive `noindex`.

## Vérification

Depuis la racine du dépôt :

```fish
make verify
```

Cette commande vérifie la cohérence de génération, OpenAPI, le formatage et l'analyse statique Go, les tests unitaires, le formatage / lint / types / tests de composants / builds des deux interfaces, ainsi que les responsabilités des tables et les frontières du code.

Les tests d'intégration PostgreSQL nécessitent `MODURA_TEST_DATABASE_URL` vers une base dédiée dont le nom se termine par `_test`. Ils réinitialisent son schéma `modura`. Ne jamais utiliser une base métier.

```fish
make backend-test-integration
```

Les tests navigateur du site public utilisent Chromium déjà installé et des données API de test fixes :

```fish
make web-e2e
```

Ils vérifient les interactions et ne remplacent pas les tests de base de données. Les tests E2E d'administration utilisent `make admin-e2e` et exigent une base nommée `modura_test`. `make verify-release` ajoute les contrôles de vulnérabilités et de licences des dépendances.

## Contribuer

Lire [AGENTS.md](AGENTS.md), puis les modules existants et le [contrat OpenAPI](api/openapi.yaml). Mettre à jour ensemble contrat HTTP, implémentation, clients générés et tests. Ne pas modifier manuellement les fichiers générés.

Les documents de travail d'architecture et de produit sont actuellement hors du dépôt ; les anciens liens de documentation peuvent être indisponibles. Ne pas committer de secrets locaux, configurations d'environnement, justificatifs de résidence ou données privées d'utilisateurs.

Les problèmes et suggestions sont les bienvenus dans les [Issues](https://github.com/psmMRFP/WhereToLive/issues), ainsi que les pull requests. Maintenir les cinq README synchronisés lors des changements de statut ou d'instructions de développement.

## Licence

[GNU AGPL v3.0](LICENSE) (`AGPL-3.0-only`). Les dépendances et les sources de données conservent leurs licences respectives.
