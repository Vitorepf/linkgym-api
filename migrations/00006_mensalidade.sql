-- +goose Up

-- MENSALIDADE, como o CONTEXT.md define: "valor combinado e dia do vencimento daquele
-- Vínculo, digitados uma vez pelo personal; ele marca pago ou em aberto, e o app nunca
-- cobra". Uma linha por Vínculo — o valor não é do aluno nem do time, é do combinado
-- entre os dois.
--
-- Centavos em INTEGER: dinheiro em float é o bug que ninguém acha. due_day até 28 para
-- existir em todo mês civil sem caso especial de fevereiro.
CREATE TABLE mensalidades (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    bond_id      uuid NOT NULL UNIQUE REFERENCES bonds(id) ON DELETE CASCADE,
    amount_cents integer NOT NULL CHECK (amount_cents >= 0),
    due_day      integer NOT NULL CHECK (due_day BETWEEN 1 AND 28),
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);

-- "Ele marca pago": o pagamento é um FATO por mês civil, não um estado que muda de ideia.
-- Em aberto = ausência de linha do mês — a mesma regra do produto inteiro, onde falha é
-- ausência de marca. month é sempre o dia 1 do mês (CHECK), então unique(bond, month)
-- significa "um pagamento por mês" sem janela de fuso.
CREATE TABLE mensalidade_pagamentos (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    bond_id    uuid NOT NULL REFERENCES bonds(id) ON DELETE CASCADE,
    month      date NOT NULL CHECK (date_trunc('month', month)::date = month),
    paid_at    timestamptz NOT NULL DEFAULT now(),
    UNIQUE (bond_id, month)
);

CREATE INDEX mensalidade_pagamentos_bond ON mensalidade_pagamentos (bond_id, month DESC);

-- +goose Down

DROP TABLE mensalidade_pagamentos;
DROP TABLE mensalidades;
