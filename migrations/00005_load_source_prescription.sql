-- +goose Up

-- QUARTO valor de load_source: 'prescription'.
--
-- A coluna existe para o personal ver DE ONDE veio o número, e ela colapsava dois degraus
-- diferentes num único 'history': o que o CORPO levantou (workout_sets) e o que o personal
-- PRESCREVEU e o corpo não executou (prescription_items da ficha anterior). O segundo
-- herdava o carimbo do primeiro, e o personal lia "veio da última série" num número que
-- aquele corpo nunca levantou. Fato e intenção não podem ter o mesmo nome.
--
-- O nome é a palavra que a própria tabela já usa: Prescrição é "Modelo aplicado a uma
-- Pessoa, com cargas e detalhes daquela pessoa". Nenhuma palavra de domínio nova entra.
--
-- Não destrutiva: só amplia o CHECK. Nenhuma linha muda, nenhum valor antigo deixa de valer.
ALTER TABLE prescription_items DROP CONSTRAINT prescription_items_load_source;
ALTER TABLE prescription_items ADD CONSTRAINT prescription_items_load_source
    CHECK (load_source IN ('history', 'starter', 'manual', 'prescription'));

COMMENT ON COLUMN prescription_items.load_source IS
    'De onde veio a carga: history = série que o corpo executou; prescription = a ficha anterior daquela Pessoa; starter = chute do Modelo; manual = o personal cravou.';

-- +goose Down

-- Voltar é que perde: o esquema antigo não tem onde guardar "veio da ficha anterior", e
-- 'history' é o carimbo que a cascata antiga escrevia nesse caso. A distinção some.
UPDATE prescription_items SET load_source = 'history' WHERE load_source = 'prescription';
ALTER TABLE prescription_items DROP CONSTRAINT prescription_items_load_source;
ALTER TABLE prescription_items ADD CONSTRAINT prescription_items_load_source
    CHECK (load_source IN ('history', 'starter', 'manual'));
