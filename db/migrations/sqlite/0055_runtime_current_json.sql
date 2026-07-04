-- +goose Up
-- P3-3.1 (аудит #3): узкоколоночное зеркало AgentRuntime теряло ~44% полей
-- при рестарте панели. Runtime теперь хранится каноническим JSON-blob'ом
-- (runtime_json = json.Marshal(server.AgentRuntime)); реальными колонками
-- остаются только agent_id (PK/FK) и observed_at_unix (ORDER BY).
-- Pre-prod: без backfill — drop+recreate, первый же снапшот агента
-- заново наполняет таблицу (данные derived, источник — агент).
DROP TABLE telemt_runtime_current;
CREATE TABLE telemt_runtime_current (
    agent_id TEXT PRIMARY KEY,
    observed_at_unix INTEGER NOT NULL,
    runtime_json TEXT NOT NULL DEFAULT '',
    FOREIGN KEY (agent_id) REFERENCES agents (id) ON DELETE CASCADE
);

-- +goose Down
-- Dev-stage: drop+recreate acceptable, no rollback.
SELECT 1;
