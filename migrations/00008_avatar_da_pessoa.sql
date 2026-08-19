-- +goose Up

-- O ROSTO da Pessoa: foto (chave no object storage) ou uma cor de avatar escolhida.
-- Mora em people, não no vínculo — o rosto atravessa times, como o PR e o acervo do
-- corpo. Foto ganha da cor quando as duas existem; nenhuma das duas = iniciais, que é o
-- fallback que o app sempre teve.
ALTER TABLE people ADD COLUMN avatar_object_key text;
ALTER TABLE people ADD COLUMN avatar_color text
    CONSTRAINT people_avatar_color_hex CHECK (avatar_color ~ '^#[0-9A-Fa-f]{6}$');

COMMENT ON COLUMN people.avatar_object_key IS 'Foto da Pessoa no object storage. Ganha da cor.';
COMMENT ON COLUMN people.avatar_color IS 'Avatar sem foto: cor de fundo das iniciais.';

-- +goose Down

ALTER TABLE people DROP COLUMN avatar_object_key;
ALTER TABLE people DROP COLUMN avatar_color;
