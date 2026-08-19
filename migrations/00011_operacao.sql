-- +goose Up

-- ============================================================================
-- A OPERAÇÃO DO PERSONAL — tudo ADITIVO.
--
-- `mensalidades` não é tocada: ela É o combinado entre as duas pessoas, e toda
-- query de internal/owner/operacao.go continua idêntica letra por letra. Não há
-- expand/contract aqui porque não há nada para contrair — migrar fato financeiro
-- é a única parte irreversível de qualquer plano, e esta rodada entrega o produto
-- inteiro sem precisar dela.
-- ============================================================================


-- O VALOR CONGELA NO FATO. Hoje a Operação lê o valor de `mensalidades` na hora da
-- consulta: subir o combinado de R$300 para R$350 em maio REESCREVE o que entrou em
-- janeiro. Dinheiro do passado que muda de valor é o bug que ninguém acha — o mesmo
-- motivo de os centavos serem integer desde 00006.
ALTER TABLE mensalidade_pagamentos ADD COLUMN amount_cents integer NOT NULL DEFAULT 0;

UPDATE mensalidade_pagamentos pg
   SET amount_cents = m.amount_cents
  FROM mensalidades m
 WHERE m.bond_id = pg.bond_id;

-- POR ONDE o dinheiro entrou. 'mao' é o dedo do personal e é o ÚNICO valor que esta
-- fase escreve. O desfazer é um DELETE travado em meio = 'mao': quando existir
-- provedor, o fato dele não some por toque errado, e a regra já nasce escrita — sem
-- coluna desfeito_em, sem soft-delete, sem índice parcial. Um dedo errado não é um
-- fato, e "em aberto = ausência de linha" continua valendo.
ALTER TABLE mensalidade_pagamentos ADD COLUMN meio text NOT NULL DEFAULT 'mao'
    CHECK (meio IN ('mao', 'pix', 'cartao'));


-- O ALUNO GANHA O VERBO, e ele NÃO MARCA NADA.
--
-- Toda a Operação repousa num gesto digitado à mão, e "em aberto = ausência de linha"
-- torna o esquecimento do personal indistinguível de calote: no mês em que ele parar
-- de marcar, a tela acusa a turma inteira. O aluno é a única outra pessoa do sistema
-- com incentivo PRÓPRIO para corrigir isso. "Já paguei" é o que ela diz; o FATO
-- continua sendo do personal, no toque dele.
--
-- Uma linha por vínculo por mês (PK composta = idempotente). Some sozinha quando o
-- pagamento entra — a leitura junta com NOT EXISTS em mensalidade_pagamentos.
CREATE TABLE ja_paguei (
    bond_id uuid        NOT NULL REFERENCES bonds (id) ON DELETE CASCADE,
    month   date        NOT NULL CHECK (date_trunc('month', month)::date = month),
    dito_em timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (bond_id, month)
);


-- O EXTRA: o que o personal vende fora da mensalidade. Avaliação física,
-- whey, marmita, aula experimental — "qualquer tipo de produto" traduzido para o que
-- ele de fato faz. DUAS colunas de conteúdo, descrição livre e valor, porque o pedido
-- era "pouquíssimos cliques": categoria, foto, estoque, frete e variante são campos
-- entre ele e o dinheiro, e cada um é uma decisão que ele não sabe tomar às 22h.
-- Repetiu a mesma descrição três meses seguidos? Aí vira produto, e aí tem taxonomia.
--
-- O nome NÃO é "cobrança", e isso é lei do CONTEXT.md, não estilo: o produto recusa o
-- vocabulário de sistema de faturamento inteiro (cobrança, checkout, gateway, boleto,
-- split, inadimplente) porque o app nunca cobra o aluno — quem cobra é o personal, com a
-- frase dele. "Extra" é o que ele vendeu; o dedo dele é que registra o recebimento.
--
-- recebida_em e meio andam juntos: ou os dois são nulos (em aberto), ou nenhum.
CREATE TABLE extras (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    bond_id     uuid        NOT NULL REFERENCES bonds (id) ON DELETE CASCADE,
    descricao   text        NOT NULL CHECK (length(btrim(descricao)) BETWEEN 1 AND 60),
    valor_cents integer     NOT NULL CHECK (valor_cents > 0),
    criada_em   timestamptz NOT NULL DEFAULT now(),
    recebida_em timestamptz,
    meio        text CHECK (meio IN ('mao', 'pix', 'cartao')),
    CONSTRAINT extras_recebimento CHECK ((recebida_em IS NULL) = (meio IS NULL))
);

CREATE INDEX extras_aberta ON extras (bond_id) WHERE recebida_em IS NULL;


-- A SAÍDA, que nunca foi escrita.
--
-- `bonds.status` aceita 'ended' desde 00001 e NENHUMA linha de Go escreve isso — só
-- 'active', no aceite do convite. Consequência viva: quem cancelou em março continua
-- contando em student_count, receita_cents e ticket_cents para sempre, e os quatro
-- números da Operação sobem sozinhos. Consequência futura: sem evento de saída não
-- existe rótulo, e nenhuma afirmação sobre risco é falsificável.
--
-- DEDUZIDA do silêncio (60 dias sem sessão e sem pagamento) e CONTRADIZÍVEL pelo
-- personal: ele nunca assina atestado de óbito, só desmente. Não responder É o rótulo.
-- `ultima_presenca` é CONGELADA no ato — recalcular de tabela viva dá rótulo móvel,
-- que é exatamente o que inutiliza um dado de cancelamento.
CREATE TABLE saidas (
    bond_id         uuid PRIMARY KEY REFERENCES bonds (id) ON DELETE CASCADE,
    ultima_presenca date,
    ultimo_mes_pago date,
    confirmada_em   timestamptz NOT NULL DEFAULT now()
);


-- O TOQUE: o personal falou com alguém, e por qual motivo.
--
-- É o que torna a fila falsificável — sem ele, "assertivo" é superstição bem
-- diagramada — e é a fonte da ÚNICA estatística agregada do produto, que é sobre o
-- comportamento DELE: "você tocou em 7 pessoas este mês, 5 voltaram a treinar".
-- Gravado ao ABRIR o WhatsApp, não ao enviar: a alternativa é não gravar nada.
-- Sem coluna de score, porque não existe score.
CREATE TABLE toques (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    studio_id  uuid        NOT NULL REFERENCES studios (id) ON DELETE CASCADE,
    person_id  uuid        NOT NULL REFERENCES people (id) ON DELETE CASCADE,
    motivo     text        NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX toques_pessoa ON toques (person_id, created_at DESC);


-- A MEMÓRIA DO QUE ELE FEZ. Hoje internal/owner/attention.go faz
-- `DELETE FROM attention_items WHERE id = $1` — toda vez que o personal age sobre a
-- fila, o sistema destrói o registro de que sinalizou aquela pessoa E de que ele agiu.
-- O campo `Applied bool` do struct Attention já existe, já é serializado, e nunca foi
-- preenchido por ninguém. Uma coluna devolve o placar.
ALTER TABLE attention_items ADD COLUMN applied_at timestamptz;


-- A CHAVE PIX DO PERSONAL. O BR Code é montado pelo backend a partir daqui: o dinheiro
-- do aluno vai DIRETO para a conta dele, a LinkGym não toca nele e não entra na cadeia
-- de custódia. Nome e cidade são campos obrigatórios do payload EMV do Banco Central.
ALTER TABLE studios ADD COLUMN chave_pix        text;
ALTER TABLE studios ADD COLUMN nome_recebedor   text;
ALTER TABLE studios ADD COLUMN cidade_recebedor text;

COMMENT ON COLUMN studios.chave_pix IS
    'Chave Pix do personal. O BR Code é montado a partir dela e o dinheiro vai direto para ele: a LinkGym não custodia.';

-- +goose Down

ALTER TABLE studios DROP COLUMN cidade_recebedor;
ALTER TABLE studios DROP COLUMN nome_recebedor;
ALTER TABLE studios DROP COLUMN chave_pix;
ALTER TABLE attention_items DROP COLUMN applied_at;
DROP TABLE toques;
DROP TABLE saidas;
DROP TABLE extras;
DROP TABLE ja_paguei;
ALTER TABLE mensalidade_pagamentos DROP COLUMN meio;
ALTER TABLE mensalidade_pagamentos DROP COLUMN amount_cents;
