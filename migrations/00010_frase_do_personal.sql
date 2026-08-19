-- +goose Up

-- A FRASE DO PERSONAL, na Prescrição.
--
-- O /v1/today devolvia uma coach_line MONTADA por template ("<exercício> em <carga>.
-- Técnica, não ego.") e a tela do aluno a desenhava embaixo do rosto e do nome do
-- personal, como se fosse a voz dele. O app assinava, em nome de uma pessoa real, uma
-- frase que ela não escreveu — e a presença dela é a única coisa que este produto vende.
--
-- A frase mora aqui, e não em studios.config, porque é sobre AQUELE dia daquele corpo: a
-- config guarda o que vale para todo aluno sempre (boas_vindas, retomada), e a Prescrição
-- é o que o personal montou hoje. Uma linha por publicação, opcional.
--
-- NULL é a resposta honesta para "ele não escreveu nada", e por isso a coluna é anulável e
-- sem DEFAULT: sem frase, ninguém fala no lugar dele e o bloco simplesmente não existe na
-- tela. A regra do texto é a MESMA das outras duas frases (internal/voz): no máximo 100
-- caracteres, sem link e sem telefone.
ALTER TABLE prescriptions ADD COLUMN coach_line text;

COMMENT ON COLUMN prescriptions.coach_line IS
    'A frase que o personal escreveu ao publicar esta Prescrição, na voz dele. NULL = ele não escreveu, e o app não assina no lugar dele.';

-- +goose Down

ALTER TABLE prescriptions DROP COLUMN coach_line;
