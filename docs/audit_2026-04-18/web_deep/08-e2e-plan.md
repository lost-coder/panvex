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

## Extensions, добавленные сверху baseline (в этом же PR)

* **Wider browser matrix** — в `playwright.config.ts` теперь три
  проекта: chromium, firefox, webkit. Smoke-suite прогоняется на всех
  трёх параллельно. `test:e2e:install` тянет все три движка.
* **axe-playwright** — `tests/e2e/a11y.spec.ts` прогоняет axe-core на
  Dashboard/Servers/Clients/Settings с WCAG 2.1 A+AA тегами. Правило
  `color-contrast` временно выключено (token-level аудит — 6.11).
* **Visual regression через Playwright snapshots** —
  `tests/e2e/visual.spec.ts` делает по одному скриншоту на primary
  route. Baseline хранится в `tests/e2e/visual.spec.ts-snapshots/` и
  коммитится в репо (первый CI-прогон пишет baselines, последующие
  диффают). Обновление: `npm run test:e2e:update-snapshots`.
* **Backend-integration E2E scaffold** —
  `playwright.integration.config.ts`, `tests/e2e/integration/` с
  `login.int.spec.ts` и `README.md`. Запуск: поднять control-plane
  вручную или через (пока не закоммиченный) docker-compose, затем
  `npm run test:e2e:integration`.

## Что НЕ покрыто этой фазой (defer)

* **Chromatic / Percy** — внешний сервис, требует API-ключей. Наш
  локальный snapshot-подход покрывает 80% use-case бесплатно; на
  Chromatic/Percy перейдём, если PR-комментарии с UI-diff'ом станут
  критичны для ревью.
* **Visual baselines в репо** — первый CI-прогон должен сгенерировать
  и закоммитить их автоматически через GitHub Actions; локальная
  генерация требует `npx playwright install chromium`.
* **Integration docker-compose** — `tests/e2e/integration/docker-compose.yml`
  оставлен ненаписанным (требует выбора образа, сборочной pipeline и
  volume-mount layout'а под локальную Go-сборку). Пока run-локально
  задокументирован в README.
* **Agent-level интеграция** — агенту нужны gRPC + TLS + Telemt target.
  Отдельный тир, живёт в `cmd/agent/*_test.go` на Go, не здесь.

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
