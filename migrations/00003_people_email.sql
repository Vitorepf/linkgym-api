-- +goose Up

ALTER TABLE people ADD COLUMN email text;
CREATE UNIQUE INDEX people_email ON people (email) WHERE email IS NOT NULL;

COMMENT ON COLUMN people.email IS 'Opcional. Não é a chave da conta. Login no v1 é telefone + OTP.';

-- +goose Down

DROP INDEX IF EXISTS people_email;
ALTER TABLE people DROP COLUMN IF EXISTS email;
