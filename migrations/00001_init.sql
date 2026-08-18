-- +goose Up

CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- Pessoa
CREATE TABLE people (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    phone text NOT NULL UNIQUE,
    name text NOT NULL DEFAULT '',
    active_bond_id uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- Estúdio (marca do personal)
CREATE TABLE studios (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_person_id uuid NOT NULL REFERENCES people (id) ON DELETE RESTRICT,
    name text NOT NULL,
    accent_color text NOT NULL DEFAULT '#ec3013',
    logo_object_key text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT studios_accent_color_hex CHECK (accent_color ~ '^#[0-9A-Fa-f]{6}$'),
    CONSTRAINT studios_one_owner UNIQUE (owner_person_id)
);

-- Vínculo Pessoa ↔ Estúdio
CREATE TABLE bonds (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    person_id uuid NOT NULL REFERENCES people (id) ON DELETE CASCADE,
    studio_id uuid NOT NULL REFERENCES studios (id) ON DELETE CASCADE,
    role text NOT NULL,
    status text NOT NULL,
    onboarding jsonb NOT NULL DEFAULT '{}'::jsonb,
    commitment_text text,
    commitment_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT bonds_role CHECK (role IN ('owner', 'student')),
    CONSTRAINT bonds_status CHECK (status IN ('invited', 'active', 'paused', 'ended')),
    CONSTRAINT bonds_person_studio UNIQUE (person_id, studio_id)
);

CREATE UNIQUE INDEX bonds_one_owner_per_studio ON bonds (studio_id) WHERE role = 'owner';
CREATE INDEX bonds_studio_students ON bonds (studio_id) WHERE role = 'student' AND status = 'active';
CREATE INDEX bonds_person ON bonds (person_id);

ALTER TABLE people
    ADD CONSTRAINT people_active_bond_fk
    FOREIGN KEY (active_bond_id) REFERENCES bonds (id) ON DELETE SET NULL;

-- Convite (única porta do aluno)
CREATE TABLE invites (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    studio_id uuid NOT NULL REFERENCES studios (id) ON DELETE CASCADE,
    created_by_person_id uuid NOT NULL REFERENCES people (id) ON DELETE RESTRICT,
    phone text NOT NULL,
    code text NOT NULL UNIQUE,
    expires_at timestamptz NOT NULL,
    accepted_at timestamptz,
    accepted_person_id uuid REFERENCES people (id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX invites_studio_phone ON invites (studio_id, phone);

-- Código de 4 dígitos (hash). Sem senha.
CREATE TABLE login_codes (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    phone text NOT NULL,
    code_hash bytea NOT NULL,
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz,
    attempt_count int NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX login_codes_phone_open ON login_codes (phone) WHERE consumed_at IS NULL;

CREATE TABLE auth_sessions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    person_id uuid NOT NULL REFERENCES people (id) ON DELETE CASCADE,
    token_hash bytea NOT NULL UNIQUE,
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX auth_sessions_person ON auth_sessions (person_id);

CREATE TABLE media_objects (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    object_key text NOT NULL UNIQUE,
    content_type text NOT NULL DEFAULT 'application/octet-stream',
    byte_size bigint,
    created_by_person_id uuid REFERENCES people (id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

-- Biblioteca de movimentos do estúdio
CREATE TABLE exercises (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    studio_id uuid NOT NULL REFERENCES studios (id) ON DELETE CASCADE,
    name text NOT NULL,
    video_object_key text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX exercises_studio ON exercises (studio_id);

-- Modelo: estrutura, sem carga de aluno
CREATE TABLE models (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    studio_id uuid NOT NULL REFERENCES studios (id) ON DELETE CASCADE,
    name text NOT NULL,
    created_by_person_id uuid NOT NULL REFERENCES people (id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX models_studio ON models (studio_id);

CREATE TABLE model_items (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    model_id uuid NOT NULL REFERENCES models (id) ON DELETE CASCADE,
    exercise_id uuid NOT NULL REFERENCES exercises (id) ON DELETE RESTRICT,
    position int NOT NULL,
    planned_sets int NOT NULL,
    planned_reps text NOT NULL,
    rest_seconds int,
    notes text,
    starter_load_kg numeric(6, 2),
    CONSTRAINT model_items_position UNIQUE (model_id, position),
    CONSTRAINT model_items_sets CHECK (planned_sets > 0)
);

-- Prescrição: modelo + cargas daquela pessoa
CREATE TABLE prescriptions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    studio_id uuid NOT NULL REFERENCES studios (id) ON DELETE CASCADE,
    person_id uuid NOT NULL REFERENCES people (id) ON DELETE CASCADE,
    model_id uuid NOT NULL REFERENCES models (id) ON DELETE RESTRICT,
    for_date date NOT NULL,
    status text NOT NULL,
    published_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT prescriptions_status CHECK (status IN ('draft', 'published', 'superseded'))
);

CREATE UNIQUE INDEX prescriptions_one_published_per_day
    ON prescriptions (person_id, studio_id, for_date)
    WHERE status = 'published';

CREATE INDEX prescriptions_studio_day ON prescriptions (studio_id, for_date);

CREATE TABLE prescription_items (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    prescription_id uuid NOT NULL REFERENCES prescriptions (id) ON DELETE CASCADE,
    exercise_id uuid NOT NULL REFERENCES exercises (id) ON DELETE RESTRICT,
    position int NOT NULL,
    planned_sets int NOT NULL,
    planned_reps text NOT NULL,
    load_kg numeric(6, 2),
    rest_seconds int,
    notes text,
    load_source text NOT NULL,
    CONSTRAINT prescription_items_position UNIQUE (prescription_id, position),
    CONSTRAINT prescription_items_load_source CHECK (load_source IN ('history', 'starter', 'manual')),
    CONSTRAINT prescription_items_sets CHECK (planned_sets > 0)
);

-- Sessão: execução. client_id = idempotência do celular
CREATE TABLE workout_sessions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    person_id uuid NOT NULL REFERENCES people (id) ON DELETE CASCADE,
    studio_id uuid NOT NULL REFERENCES studios (id) ON DELETE CASCADE,
    prescription_id uuid REFERENCES prescriptions (id) ON DELETE SET NULL,
    client_id uuid NOT NULL,
    started_at timestamptz NOT NULL DEFAULT now(),
    finished_at timestamptz,
    effort numeric(3, 1),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT workout_sessions_client UNIQUE (person_id, client_id)
);

CREATE INDEX workout_sessions_person ON workout_sessions (person_id, started_at DESC);
CREATE INDEX workout_sessions_studio_day ON workout_sessions (studio_id, started_at);

CREATE TABLE workout_sets (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id uuid NOT NULL REFERENCES workout_sessions (id) ON DELETE CASCADE,
    prescription_item_id uuid REFERENCES prescription_items (id) ON DELETE SET NULL,
    exercise_id uuid NOT NULL REFERENCES exercises (id) ON DELETE RESTRICT,
    swapped_from_exercise_id uuid REFERENCES exercises (id) ON DELETE SET NULL,
    client_set_id uuid NOT NULL,
    set_index int NOT NULL,
    reps int,
    load_kg numeric(6, 2),
    rest_seconds int,
    effort numeric(3, 1),
    performed_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT workout_sets_client UNIQUE (session_id, client_set_id),
    CONSTRAINT workout_sets_index CHECK (set_index > 0)
);

CREATE INDEX workout_sets_session ON workout_sets (session_id);

-- PR: da Pessoa, não do estúdio
CREATE TABLE personal_records (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    person_id uuid NOT NULL REFERENCES people (id) ON DELETE CASCADE,
    exercise_id uuid NOT NULL REFERENCES exercises (id) ON DELETE CASCADE,
    load_kg numeric(6, 2) NOT NULL,
    reps int,
    session_id uuid REFERENCES workout_sessions (id) ON DELETE SET NULL,
    achieved_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT personal_records_person_exercise UNIQUE (person_id, exercise_id)
);

-- Ofensiva + protetor: do Vínculo (personal ativo)
CREATE TABLE streaks (
    bond_id uuid PRIMARY KEY REFERENCES bonds (id) ON DELETE CASCADE,
    current_count int NOT NULL DEFAULT 0,
    protector_available boolean NOT NULL DEFAULT true,
    protector_spent_at timestamptz,
    last_fulfilled_on date,
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT streaks_count CHECK (current_count >= 0)
);

CREATE TABLE badges (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    studio_id uuid NOT NULL REFERENCES studios (id) ON DELETE CASCADE,
    person_id uuid NOT NULL REFERENCES people (id) ON DELETE CASCADE,
    badge_key text NOT NULL,
    earned_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT badges_unique UNIQUE (studio_id, person_id, badge_key)
);

-- Atenção do dia: fila curta (app limita a ~3)
CREATE TABLE attention_items (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    studio_id uuid NOT NULL REFERENCES studios (id) ON DELETE CASCADE,
    person_id uuid NOT NULL REFERENCES people (id) ON DELETE CASCADE,
    for_date date NOT NULL,
    reason text NOT NULL,
    rank int NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT attention_items_unique UNIQUE (studio_id, person_id, for_date),
    CONSTRAINT attention_items_rank CHECK (rank > 0)
);

CREATE INDEX attention_items_day ON attention_items (studio_id, for_date, rank);

-- +goose Down

DROP TABLE IF EXISTS attention_items;
DROP TABLE IF EXISTS badges;
DROP TABLE IF EXISTS streaks;
DROP TABLE IF EXISTS personal_records;
DROP TABLE IF EXISTS workout_sets;
DROP TABLE IF EXISTS workout_sessions;
DROP TABLE IF EXISTS prescription_items;
DROP TABLE IF EXISTS prescriptions;
DROP TABLE IF EXISTS model_items;
DROP TABLE IF EXISTS models;
DROP TABLE IF EXISTS exercises;
DROP TABLE IF EXISTS media_objects;
DROP TABLE IF EXISTS auth_sessions;
DROP TABLE IF EXISTS login_codes;
DROP TABLE IF EXISTS invites;
ALTER TABLE IF EXISTS people DROP CONSTRAINT IF EXISTS people_active_bond_fk;
DROP TABLE IF EXISTS bonds;
DROP TABLE IF EXISTS studios;
DROP TABLE IF EXISTS people;
