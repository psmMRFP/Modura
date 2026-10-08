<div align="center">

# WhereToLive

### Tu próximo lugar para vivir empieza con información fiable.

Una plataforma abierta para vivir a largo plazo en otro lugar, emigrar, estudiar en el extranjero y trabajar en remoto.

[![CI](https://github.com/psmMRFP/WhereToLive/actions/workflows/ci.yml/badge.svg)](https://github.com/psmMRFP/WhereToLive/actions/workflows/ci.yml)
[![Licencia](https://img.shields.io/badge/License-AGPL%203.0-blue.svg)](LICENSE)
[![Estado](https://img.shields.io/badge/Stage-Early%20Development-orange.svg)](#estado-actual)

[English](README.md) · [简体中文](README.zh-CN.md) · [Deutsch](README.de.md) · [Français](README.fr.md) · **Español**

[¿Por qué WhereToLive?](#por-qué-wheretolive) · [Estado actual](#estado-actual) · [Desarrollo local](#desarrollo-local) · [Contribuir](#contribuir)

</div>

---

## ¿Por qué WhereToLive?

Elegir dónde vivir requiere más que una guía de viajes. Las opciones de visado, los impuestos, el alquiler, los gastos cotidianos y las experiencias de quienes han vivido allí deberían consultarse en un mismo lugar.

WhereToLive reúne **hechos verificables, experiencias de residentes y preferencias personales**, con fuentes claras, fechas de vigencia e historial de cambios.

> Cómo se valora un lugar de forma objetiva y cuánto encaja contigo son dos preguntas distintas.

| Sistema            | Pregunta                                                               | Escala prevista                  |
| ------------------ | ---------------------------------------------------------------------- | -------------------------------- |
| **Data Score**     | ¿Qué indican los datos objetivos?                                      | 0–100, con detalle por dimensión |
| **Resident Score** | ¿Qué opinan los residentes verificados?                                | 1–10, con ajuste bayesiano       |
| **Your Fit**       | ¿Encaja con tu presupuesto, necesidades lingüísticas y estilo de vida? | Según tus propias ponderaciones  |

Los tres sistemas son independientes y todavía no están implementados. **Modura Atlas** es el nombre en clave de la transformación; **WhereToLive** es el nombre del producto.

## Estado actual

El proyecto está en una fase inicial de desarrollo y se basa en Modura. El catálogo público de lugares y la base operativa funcionan; todavía no hay un conjunto de datos de lugares para producción.

| Implementado              | Funcionalidades                                                                                                 |
| ------------------------- | --------------------------------------------------------------------------------------------------------------- |
| Sitio público             | Búsqueda de lugares sin cuenta, paginación y páginas de detalle                                                 |
| Interfaz multilingüe      | English, 简体中文, Deutsch, Français, Español                                                                   |
| Modelo geográfico         | Países, regiones, ciudades, barrios, islas, slugs estables y alias multilingües                                 |
| Administración de lugares | Borradores, edición, publicación, retirada, protección frente a conflictos de versión y auditoría transaccional |
| Contrato API              | OpenAPI compartido entre Go y ambos frontends, con tipos y clientes de consultas generados                      |
| Inicialización de la base | Creación automática de una base dedicada ausente; se prohíben el rol y la base predeterminados `postgres`       |

**Próximos pasos:** registro público y verificación del correo → fuentes, evidencias y versiones de hechos → Research Agent → visados, impuestos y coste de vida → comentarios y sugerencias unificados → verificación de residencia y reseñas → adecuación personal.

Está prevista la traducción de reseñas mediante IA cuando el idioma de lectura difiera del original. Los usuarios podrán activar la traducción automática y consultar siempre el texto original.

Las dimensiones de información y puntuación se gestionarán desde la administración, con controles separados para mostrar y puntuar. La compatibilidad con Web3 / criptomonedas y los controles de divisas / capitales son dimensiones previstas. Esta capacidad de configuración aún no está implementada.

## Principios del producto

- **Primero las evidencias:** la IA ayuda a investigar, extraer, traducir y comparar; no es una fuente de información.
- **Historial trazable:** los hechos sensibles al tiempo conservan versiones, fuentes, fechas de vigencia y fechas de verificación.
- **Información diferenciada:** hechos oficiales, experiencias de la comunidad y alertas actuales se presentan por separado.
- **Minimización de datos personales:** los documentos de residencia son privados, no se envían por defecto a modelos de terceros y se eliminan después de la revisión; se conservan los metadatos necesarios de verificación.
- **Independencia comercial:** la publicidad no puede influir en la prioridad de cobertura, Data Score, Resident Score ni Your Fit.

Estos son compromisos de diseño; las funciones correspondientes se desarrollan de forma gradual.

## Tecnología y estructura

**Monolito modular en Go + PostgreSQL + React.** Los módulos de negocio se comunican mediante llamadas locales. La búsqueda inicial utiliza PostgreSQL, sin un clúster de búsqueda independiente ni una arquitectura de microservicios.

```text
WhereToLive/
├── backend/     API Go, módulos de negocio, consultas SQL y migraciones
├── web/         Sitio público React
├── admin/       Interfaz React de operaciones y moderación
├── api/         Contrato HTTP de referencia y configuración de generación
├── scripts/     Comprobaciones de contratos, propiedad de tablas y límites del código
└── .github/     Flujos de CI
```

Los frontends utilizan React, Vite, React Router, TanStack Query y Ant Design. Las consultas SQL usan sqlc; los tipos HTTP y los clientes se generan desde OpenAPI.

## Desarrollo local

### 1. Preparar el entorno

La configuración del repositorio es la referencia: actualmente **Go 1.27, Node.js ≥ 26 y npm ≥ 12**. La verificación completa también requiere `oapi-codegen`, `sqlc`, `golangci-lint`, Python 3 y Make instalados. Las versiones fijadas aparecen en la [configuración de CI](.github/workflows/ci.yml), que utiliza PostgreSQL 17.

Instalar las dependencias frontend fijadas desde la raíz del repositorio:

```fish
npm ci --prefix admin
npm ci --prefix web
```

### 2. Configurar el backend

Consultar [backend/.env.example](backend/.env.example). Proporcionar la configuración mediante el entorno del proceso o el mecanismo de secretos del despliegue. La aplicación no carga archivos `.env` automáticamente.

| Variable                      | Función                                                                                      |
| ----------------------------- | -------------------------------------------------------------------------------------------- |
| `MODURA_DATABASE_URL`         | URL de conexión para un rol PostgreSQL dedicado y una base con nombre explícito; obligatoria |
| `MODURA_AUTH_SIGNING_KEY`     | Clave de firma de al menos 32 bytes; obligatoria                                             |
| `MODURA_AUTH_COOKIE_SECURE`   | `false` para desarrollo HTTP local; `true` con TLS en producción                             |
| `MODURA_DATABASE_AUTO_CREATE` | `true` por defecto; puede desactivarse tras crear la base                                    |

Solo se intenta crear la base cuando PostgreSQL indica explícitamente que el destino no existe. Se conecta mediante `template1` y se crea desde `template0`; el rol dedicado necesita `CREATEDB`. **Crear la base no aplica las migraciones del esquema.**

Inicializar una base vacía sin configurar la clave de firma:

```fish
cd backend
go run ./cmd/modura-db-init
```

Después, aplicar las [migraciones](backend/internal/platform/database/migrations) en orden con una herramienta compatible con `golang-migrate`. Iniciar la API desde `backend/`:

```fish
go run ./cmd/modura
```

### 3. Iniciar los frontends

Ejecutar cada comando en una terminal independiente desde la raíz del repositorio:

```fish
npm run dev --prefix web
```

```fish
npm run dev --prefix admin
```

| Servicio       | Dirección local         |
| -------------- | ----------------------- |
| Sitio público  | `http://localhost:5174` |
| Administración | `http://localhost:5173` |
| API backend    | `http://localhost:8080` |

Los servidores de desarrollo redirigen `/api` al backend. Las páginas de lugares usan `/{locale}/places/{slug}`; cambiar de idioma conserva el slug. Sin lugares publicados se muestra un catálogo vacío. El sitio público mantiene actualmente `noindex`.

## Verificación

Desde la raíz del repositorio:

```fish
make verify
```

Comprueba la coherencia de generación, OpenAPI, formato y análisis estático Go, pruebas unitarias, formato / lint / tipos / pruebas de componentes / compilaciones de ambos frontends, propiedad de tablas y límites del código.

Las pruebas de integración PostgreSQL requieren `MODURA_TEST_DATABASE_URL` para una base dedicada cuyo nombre termine en `_test`. Restablecen su esquema `modura`. Nunca deben apuntar a una base de negocio.

```fish
make backend-test-integration
```

Las pruebas de navegador del sitio público usan Chromium ya instalado y datos API de prueba fijos:

```fish
make web-e2e
```

Verifican las interacciones y no sustituyen las pruebas de base de datos. Las pruebas E2E de administración utilizan `make admin-e2e` y exigen una base llamada `modura_test`. `make verify-release` añade las comprobaciones de vulnerabilidades y licencias de dependencias.

## Contribuir

Leer [AGENTS.md](AGENTS.md), revisar los módulos existentes y el [contrato OpenAPI](api/openapi.yaml). Actualizar juntos el contrato HTTP, la implementación, los clientes generados y las pruebas. No editar manualmente archivos generados.

Los documentos de trabajo de arquitectura y producto están fuera del repositorio; algunos enlaces históricos pueden no estar disponibles. No incluir secretos locales, configuraciones de entorno, documentos de residencia ni datos privados de usuarios en los commits.

Se pueden enviar problemas y sugerencias mediante [Issues](https://github.com/psmMRFP/WhereToLive/issues), o contribuir con un pull request. Mantener sincronizadas las cinco versiones del README al actualizar el estado o las instrucciones de desarrollo.

## Licencia

[GNU AGPL v3.0](LICENSE) (`AGPL-3.0-only`). Las dependencias y fuentes de datos conservan sus propias licencias.
