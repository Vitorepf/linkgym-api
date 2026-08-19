-- +goose Up

-- A FÁBRICA white-label: a configuração do app que o personal vende como dele.
-- JSONB com whitelist validada na API (internal/owner/config.go) — o banco guarda o
-- documento inteiro, a API só aceita chaves e valores do cardápio. Liberdade em
-- cardápio, nunca em controle contínuo: é o que garante que toda combinação sai com
-- qualidade sem revisão humana.
ALTER TABLE studios ADD COLUMN config jsonb NOT NULL DEFAULT '{}';

COMMENT ON COLUMN studios.config IS 'Config white-label do Time: liga, selos, xp, prontidao, passo_kg, dias_padrao, boas_vindas, retomada. Whitelist na API.';

-- +goose Down

ALTER TABLE studios DROP COLUMN config;
