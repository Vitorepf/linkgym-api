-- +goose Up

-- ============================================================================
-- A ASSINATURA COMEÇA A REPETIR.
--
-- O tipo `assinatura` existia desde 00012 e dizia, no próprio cardápio, "repete todo mês" —
-- e nada repetia. Vender a marmita criava UMA venda, uma vez, e no mês seguinte não havia
-- nada. O rótulo prometia um comportamento que o produto não tinha.
--
-- A MÁQUINA JÁ EXISTE e não se inventa outra: a Mensalidade resolve recorrência há duas
-- migrations DERIVANDO as competências abertas (generate_series menos os pagamentos), sem
-- gerar linha nenhuma por antecipação. Nada de cron, nada de job, nada de "gerar cobrança do
-- mês" — que é exatamente o tipo de processo que, quando falha às 3h do dia 1º, deixa a
-- turma inteira sem cobrança e ninguém percebe até o dia 5.
--
-- Uma assinatura é, então, o mesmo objeto que a mensalidade, com dois nomes a mais: de qual
-- PRODUTO ela é, e quando ela terminou.
-- ============================================================================

CREATE TABLE assinaturas (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    bond_id    uuid    NOT NULL REFERENCES bonds (id) ON DELETE CASCADE,
    produto_id uuid    NOT NULL REFERENCES produtos (id) ON DELETE RESTRICT,
    -- O VALOR CONGELA NA ASSINATURA, não no produto. Subir a marmita de R$890 para R$950
    -- não pode reescrever o que ela combinou em março — é a mesma lei de
    -- `mensalidade_pagamentos.amount_cents`, e o motivo de o preço morar aqui.
    valor_cents integer NOT NULL CHECK (valor_cents > 0),
    -- A primeira competência. Dia 1 do mês, como toda competência do produto.
    desde date NOT NULL CHECK (date_trunc('month', desde)::date = desde),
    -- A última competência COBRADA. NULL = ativa. Cancelar não apaga: o histórico do que
    -- ela pagou continua inteiro, e é por isso que isto é uma data e não um DELETE.
    ate        date CHECK (ate IS NULL OR date_trunc('month', ate)::date = ate),
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT assinaturas_janela CHECK (ate IS NULL OR ate >= desde)
);

-- Uma assinatura ATIVA por produto por pessoa. Assinar duas vezes a mesma marmita é dedo
-- duplo, não duas marmitas — a mesma lei do `ja_paguei` e do `mensalidade_pagamentos`.
CREATE UNIQUE INDEX assinaturas_uma_ativa ON assinaturas (bond_id, produto_id) WHERE ate IS NULL;
CREATE INDEX assinaturas_do_vinculo ON assinaturas (bond_id) WHERE ate IS NULL;

COMMENT ON COLUMN assinaturas.ate IS
    'Última competência cobrada. NULL = ativa. Cancelar é gravar uma data, nunca apagar: o que ela pagou continua sendo fato.';


-- A COMPETÊNCIA da venda. Sem ela não há como dizer "esta linha é a marmita de agosto", e
-- sem isso a mesma competência seria cobrada duas vezes a cada leitura da tela.
--
-- NULL para o avulso: uma avaliação física não pertence a mês nenhum, acontece uma vez. É a
-- mesma distinção que separa `mensalidades` de `extras`, agora dentro de `extras`.
ALTER TABLE extras ADD COLUMN competencia date
    CHECK (competencia IS NULL OR date_trunc('month', competencia)::date = competencia);

-- A IDEMPOTÊNCIA da recorrência, no schema e não no código: a competência de uma assinatura
-- só pode ser gravada uma vez. É o que permite a geração ser burra e repetível — chamar duas
-- vezes escreve uma linha, como o `ON CONFLICT DO NOTHING` do pagamento já faz.
CREATE UNIQUE INDEX extras_uma_por_competencia
    ON extras (bond_id, produto_id, competencia)
    WHERE competencia IS NOT NULL;

-- +goose Down

DROP INDEX extras_uma_por_competencia;
ALTER TABLE extras DROP COLUMN competencia;
DROP TABLE assinaturas;
