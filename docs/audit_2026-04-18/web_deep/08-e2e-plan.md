# Phase 8 — E2E и regression gates

## Что сделано в этом PR

* `web/playwright.config.ts` — конфиг Playwright 1.50, Chromium-only,
  webServer через `npm run dev`, trace/screenshot on failure.
* `web/tests/e2e/helpers/mock-api.ts` — набор помощников для
  интерсепции `/api/*` через `page.route()`. Тесты остаются hermetic,
  бэкенд не требуется.
* `web/tests/e2e/login.spec.ts` — happy-path + invalid credentials.
* `web/tests/e2e/navigation.spec.ts` — shortcut-навигация `g s`/`g c` и
  overlay `?`.
* `web/tests/e2e/offline.spec.ts` — проверка OfflineBanner через
  `context.setOffline()`.
* `web/package.json` — `test:e2e`, `test:e2e:install`, devDependency
  `@playwright/test`.
* `web/tsconfig.json` — `tests/e2e/**` и `playwright.config.ts` в
  `exclude` (собственный tsconfig для Playwright-тестов не нужен —
  Playwright использует собственный ts-node хост).

## Как запускать локально

```bash
cd web
npm install
npm run test:e2e:install   # один раз, чтобы скачать Chromium
npm run test:e2e
```

`webServer` в конфиге автоматически поднимает dev-сервер на :5173,
а `reuseExistingServer: !CI` означает, что локально вы можете держать
`npm run dev` отдельно — Playwright подхватит.

## Что НЕ покрыто этой фазой (defer)

* **Visual regression** (Chromatic/Percy/loki). Отдельный PR после
  стабилизации layout'а, иначе каждый UI-tweak генерирует шум.
* **Полный matrix браузеров**: Firefox и WebKit включаем только после
  того, как smoke-suite перестанет краснеть на Chromium.
* **Backend-integration E2E**: для hot-path'ов (rollout job, enrollment)
  нужен docker-compose с реальным Postgres и агентом. Прописывается
  как `e2e-integration` job отдельно от smoke'а.
* **A11y audit в Playwright**: `@axe-core/playwright` прикрутим в
  отдельном PR вместе с Storybook-level проверками (`@storybook/addon-a11y`
  уже в devDeps).

## CI

Черновой workflow лежит в `.github/workflows/e2e.yml` (draft — не
включён в required checks, пока прогон стабильный не подтверждён
хотя бы в трёх зелёных билдах подряд).

## Следующие шаги

1. Добавить smoke per feature slice: create/edit/delete client,
   create user, create enrollment token, appearance toggle.
2. После merge `feat/web-merge-ui` — включить workflow в required
   checks для main.
3. Visual regression (separate PR).
