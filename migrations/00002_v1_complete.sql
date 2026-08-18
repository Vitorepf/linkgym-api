-- +goose Up

ALTER TABLE exercises
    ADD CONSTRAINT exercises_studio_name UNIQUE (studio_id, name);

ALTER TABLE models
    ADD CONSTRAINT models_studio_name UNIQUE (studio_id, name);

ALTER TABLE workout_sessions
    ADD COLUMN synced_at timestamptz;

-- XP da Ofensiva: do Vínculo (personal ativo), não da Pessoa
CREATE TABLE xp_ledger (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    bond_id uuid NOT NULL REFERENCES bonds (id) ON DELETE CASCADE,
    amount int NOT NULL,
    reason text NOT NULL,
    session_id uuid REFERENCES workout_sessions (id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT xp_ledger_amount CHECK (amount > 0)
);

CREATE INDEX xp_ledger_bond ON xp_ledger (bond_id, created_at DESC);

-- Hoje · prontidão (corpo da Pessoa)
CREATE TABLE readiness_logs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    person_id uuid NOT NULL REFERENCES people (id) ON DELETE CASCADE,
    for_date date NOT NULL,
    energy int,
    soreness int,
    sleep int,
    note text,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT readiness_logs_day UNIQUE (person_id, for_date),
    CONSTRAINT readiness_logs_energy CHECK (energy IS NULL OR energy BETWEEN 1 AND 5),
    CONSTRAINT readiness_logs_soreness CHECK (soreness IS NULL OR soreness BETWEEN 1 AND 5),
    CONSTRAINT readiness_logs_sleep CHECK (sleep IS NULL OR sleep BETWEEN 1 AND 5)
);

CREATE TABLE push_devices (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    person_id uuid NOT NULL REFERENCES people (id) ON DELETE CASCADE,
    platform text NOT NULL,
    token text NOT NULL UNIQUE,
    created_at timestamptz NOT NULL DEFAULT now(),
    last_seen_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT push_devices_platform CHECK (platform IN ('ios', 'android'))
);

CREATE INDEX push_devices_person ON push_devices (person_id);

-- Fila de jobs no Postgres (vazia no v1; IA entra depois, fora do request)
CREATE TABLE jobs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    kind text NOT NULL,
    payload jsonb NOT NULL DEFAULT '{}'::jsonb,
    run_at timestamptz NOT NULL DEFAULT now(),
    locked_at timestamptz,
    done_at timestamptz,
    error text,
    attempt_count int NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX jobs_due ON jobs (run_at) WHERE done_at IS NULL AND locked_at IS NULL;

-- Foto de evolução: da Pessoa
CREATE TABLE progress_photos (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    person_id uuid NOT NULL REFERENCES people (id) ON DELETE CASCADE,
    object_key text NOT NULL REFERENCES media_objects (object_key) ON DELETE CASCADE,
    taken_at timestamptz NOT NULL DEFAULT now(),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX progress_photos_person ON progress_photos (person_id, taken_at DESC);

-- Avisos ao personal (troca de exercício, sessão subiu, aluno parou)
CREATE TABLE session_alerts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    studio_id uuid NOT NULL REFERENCES studios (id) ON DELETE CASCADE,
    person_id uuid NOT NULL REFERENCES people (id) ON DELETE CASCADE,
    session_id uuid REFERENCES workout_sessions (id) ON DELETE SET NULL,
    kind text NOT NULL,
    payload jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    read_at timestamptz,
    CONSTRAINT session_alerts_kind CHECK (kind IN ('exercise_swap', 'session_synced', 'student_stopped'))
);

CREATE INDEX session_alerts_studio ON session_alerts (studio_id, created_at DESC) WHERE read_at IS NULL;

-- Retomada (D11): do Vínculo, sem apagar o acervo da Pessoa
CREATE TABLE comebacks (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    bond_id uuid NOT NULL REFERENCES bonds (id) ON DELETE CASCADE,
    missed_on date NOT NULL,
    shown_at timestamptz,
    completed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT comebacks_unique UNIQUE (bond_id, missed_on)
);

-- Fio: prescrito vs feito por aluno, por dia
CREATE VIEW fio_days AS
SELECT DISTINCT ON (pr.id)
    pr.id AS prescription_id,
    pr.person_id,
    pr.studio_id,
    pr.for_date,
    pr.model_id,
    ws.id AS session_id,
    ws.started_at,
    ws.finished_at,
    ws.synced_at,
    CASE
        WHEN ws.finished_at IS NOT NULL THEN 'done'
        WHEN ws.id IS NOT NULL THEN 'in_progress'
        ELSE 'missing'
    END AS status
FROM prescriptions pr
LEFT JOIN workout_sessions ws ON ws.prescription_id = pr.id
WHERE pr.status = 'published'
ORDER BY pr.id, ws.started_at DESC NULLS LAST;

COMMENT ON TABLE people IS 'Pessoa: conta humana (telefone). Acervo do corpo mora aqui.';
COMMENT ON TABLE studios IS 'Estúdio: marca do personal (nome, logo, cor).';
COMMENT ON TABLE bonds IS 'Vínculo Pessoa ↔ Estúdio. Ativo escolhe marca e ficha.';
COMMENT ON TABLE invites IS 'Convite: única porta do aluno.';
COMMENT ON TABLE login_codes IS 'Código de 4 dígitos (hash). Sem senha.';
COMMENT ON TABLE auth_sessions IS 'Sessão de login da Pessoa.';
COMMENT ON TABLE media_objects IS 'Caminho no object storage. Postgres não guarda o arquivo.';
COMMENT ON TABLE exercises IS 'Movimento da biblioteca do Estúdio.';
COMMENT ON TABLE models IS 'Modelo: estrutura do treino. Lote replica isto, nunca a carga.';
COMMENT ON TABLE model_items IS 'Item do Modelo. starter_load_kg só para estreia sem histórico.';
COMMENT ON TABLE prescriptions IS 'Prescrição: modelo + cargas daquela Pessoa.';
COMMENT ON TABLE prescription_items IS 'Carga individual. load_source impede copiar peso de outro aluno.';
COMMENT ON TABLE workout_sessions IS 'Sessão executada. client_id = sync offline.';
COMMENT ON TABLE workout_sets IS 'Série feita. Cliente ganha se o servidor ainda não tinha.';
COMMENT ON TABLE personal_records IS 'PR da Pessoa. Sem studio_id de propósito.';
COMMENT ON TABLE streaks IS 'Ofensiva + protetor do Vínculo.';
COMMENT ON TABLE badges IS 'Selo do Estúdio ativo.';
COMMENT ON TABLE attention_items IS 'Atenção do dia. App limita a ~3.';
COMMENT ON TABLE xp_ledger IS 'XP da Ofensiva, no Vínculo.';
COMMENT ON TABLE readiness_logs IS 'Prontidão do dia (Pessoa).';
COMMENT ON TABLE push_devices IS 'Token de push da Pessoa.';
COMMENT ON TABLE jobs IS 'Fila no Postgres. Vazia no v1.';
COMMENT ON TABLE progress_photos IS 'Foto de evolução da Pessoa.';
COMMENT ON TABLE session_alerts IS 'Aviso ao personal (troca, sync, aluno parado).';
COMMENT ON TABLE comebacks IS 'Retomada curta. Não zera o acervo.';
COMMENT ON VIEW fio_days IS 'Fio: prescrito vs feito, por aluno, por dia.';

-- +goose Down

DROP VIEW IF EXISTS fio_days;
DROP TABLE IF EXISTS comebacks;
DROP TABLE IF EXISTS session_alerts;
DROP TABLE IF EXISTS progress_photos;
DROP TABLE IF EXISTS jobs;
DROP TABLE IF EXISTS push_devices;
DROP TABLE IF EXISTS readiness_logs;
DROP TABLE IF EXISTS xp_ledger;
ALTER TABLE workout_sessions DROP COLUMN IF EXISTS synced_at;
ALTER TABLE models DROP CONSTRAINT IF EXISTS models_studio_name;
ALTER TABLE exercises DROP CONSTRAINT IF EXISTS exercises_studio_name;
