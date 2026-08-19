-- +goose Up

-- O dia FECHADO não pode ser desfeito por uma sessão aberta depois.
--
-- A fio_days pegava a sessão MAIS RECENTE por prescrição (started_at DESC): o aluno
-- fechava a sessão do dia, abria outra por engano (retomar, testar, sincronizar de outro
-- aparelho) e o Painel do personal voltava a dizer "0 de 3" — o Cumprimento sumia da
-- leitura, embora continuasse valendo na Ofensiva. Quem decide o status do dia é a MELHOR
-- sessão do dia: fechada ganha de aberta, e entre iguais a mais recente fala.
CREATE OR REPLACE VIEW fio_days AS
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
ORDER BY pr.id, (ws.finished_at IS NOT NULL) DESC, ws.started_at DESC NULLS LAST;

-- +goose Down

CREATE OR REPLACE VIEW fio_days AS
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
