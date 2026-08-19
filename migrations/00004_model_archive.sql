-- +goose Up

-- Modelo apagado é Prescrição apagada, e a Prescrição é de OUTRA pessoa: prescriptions.
-- model_id é ON DELETE RESTRICT porque cada uma é o Modelo aplicado a UM corpo, com as
-- cargas daquele corpo. Então o DELETE arquiva: a linha fica, some da biblioteca de quem
-- monta, e quem já treinou continua com a ficha inteira. Aditiva, não apaga dado nenhum.
ALTER TABLE models ADD COLUMN archived_at timestamptz;

COMMENT ON COLUMN models.archived_at IS 'Fora da biblioteca. A linha fica porque prescriptions apontam para ela.';

-- O nome volta a ficar livre depois de arquivar. Sem isto, errar o nome uma vez o
-- queimaria para sempre: o UNIQUE cru continuaria contando a linha arquivada, e o personal
-- não conseguiria escrever de novo o nome que ele acabou de tirar da lista.
ALTER TABLE models DROP CONSTRAINT models_studio_name;
CREATE UNIQUE INDEX models_studio_name ON models (studio_id, name) WHERE archived_at IS NULL;

-- +goose Down

DROP INDEX IF EXISTS models_studio_name;
ALTER TABLE models ADD CONSTRAINT models_studio_name UNIQUE (studio_id, name);
ALTER TABLE models DROP COLUMN IF EXISTS archived_at;
