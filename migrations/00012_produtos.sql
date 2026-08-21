-- +goose Up

-- ============================================================================
-- O QUE O PERSONAL VENDE, com nome e preço — o catálogo.
--
-- A rodada anterior entregou só o EXTRA de dois campos (descrição livre + valor) e recusou
-- o catálogo, com o argumento de que "qualquer tipo de produto" é o pedido de uma Shopify e
-- que 28 alunos não são um marketplace. O argumento continua valendo contra CARRINHO, BUSCA,
-- CATEGORIA, VARIANTE, CUPOM e FRETE — e nenhuma dessas coisas existe aqui.
--
-- O que ele NÃO cobria, e é o que o dono do produto pediu: o personal que vende a MESMA
-- coisa toda semana (whey, marmita, avaliação, pacote de 10) redigitava nome e preço a cada
-- venda. Produto é o nome e o preço digitados UMA vez; a venda continua nominal, para uma
-- pessoa, escolhida por ele.
--
-- QUATRO TIPOS, fechados. Não é taxonomia: são três comportamentos que o dinheiro tem.
--   assinatura — repete todo mês (marmita, consultoria)
--   pacote     — carrega saldo contável (10 sessões, 4 avaliações)
--   avulso     — acontece uma vez (avaliação, aula experimental)
--   fisico     — precisa ser entregue na mão (whey, camiseta)
-- O quinto tipo que alguém quiser é sinal de que a lista está errada, não de que falta um.
-- ============================================================================

CREATE TABLE produtos (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    studio_id   uuid        NOT NULL REFERENCES studios (id) ON DELETE CASCADE,
    tipo        text        NOT NULL CHECK (tipo IN ('assinatura', 'pacote', 'avulso', 'fisico')),
    nome        text        NOT NULL CHECK (length(btrim(nome)) BETWEEN 1 AND 60),
    preco_cents integer     NOT NULL CHECK (preco_cents > 0),
    -- Só para `pacote`: quantas sessões o saldo carrega. O CHECK amarra os dois, senão um
    -- pacote sem número é um produto que ninguém sabe consumir.
    sessoes integer CHECK (sessoes IS NULL OR sessoes > 0),
    -- ESTOQUE NÃO EXISTE, e a ausência é a decisão. Quantidade exige decremento atômico,
    -- reserva, e a pergunta "o que acontece quando dois compram o último" — e o personal
    -- que tem quatro potes de whey no porta-malas resolve isso PAUSANDO o produto num
    -- toque. `ativo` é o estoque dele.
    ativo      boolean     NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT produtos_pacote_tem_sessoes CHECK ((tipo = 'pacote') = (sessoes IS NOT NULL))
);

CREATE UNIQUE INDEX produtos_nome_do_time ON produtos (studio_id, lower(btrim(nome))) WHERE ativo;
CREATE INDEX produtos_do_time ON produtos (studio_id) WHERE ativo;

COMMENT ON COLUMN produtos.ativo IS
    'Pausado some da loja do aluno e some do cardápio de venda. É o estoque do personal: um toque quando acaba o whey.';


-- A VENDA vira uma tabela só. `extras` já era o livro do que ele vende fora da mensalidade;
-- agora a linha pode APONTAR para um produto do catálogo, e a que não aponta continua sendo
-- o avulso de dois campos que já funciona. Dois caminhos para "dinheiro que não é a
-- mensalidade" divergiriam na primeira semana.
--
-- O PREÇO E O NOME CONGELAM NA LINHA. Subir o whey de R$ 219 para R$ 239 não pode reescrever
-- o que a aluna pagou mês passado — é a mesma lei que `mensalidade_pagamentos.amount_cents`
-- já obedece, e o motivo pelo qual `descricao` continua sendo coluna e não uma junção.
ALTER TABLE extras ADD COLUMN produto_id uuid REFERENCES produtos (id) ON DELETE SET NULL;

-- A TAXA DA PLATAFORMA, congelada no ato da venda.
--
-- Zero enquanto o dinheiro não passa por dentro: na fase do Pix direto, o valor vai inteiro
-- para a conta do personal e não há o que cobrar. A coluna nasce agora, e com DEFAULT 0,
-- porque a alternativa é descobrir em produção que o histórico não sabe quanto foi cobrado —
-- e taxa recalculada a partir de um percentual guardado em outro lugar é a forma clássica de
-- o extrato do personal não bater com o nosso.
ALTER TABLE extras ADD COLUMN taxa_cents integer NOT NULL DEFAULT 0 CHECK (taxa_cents >= 0);

COMMENT ON COLUMN extras.taxa_cents IS
    'O que a plataforma reteve nesta venda, em centavos, CONGELADO no ato. Zero quando o dinheiro não passou por dentro.';

CREATE INDEX extras_por_produto ON extras (produto_id) WHERE produto_id IS NOT NULL;


-- O SALDO DO PACOTE é count(*), nunca uma coluna que decrementa.
--
-- É a mesma lei que o produto já aplica em `mensalidade_pagamentos` ("em aberto = ausência
-- de linha") e na Ofensiva: contador que anda sozinho diverge em silêncio e ninguém sabe
-- dizer quando começou. Aqui, "sobraram 3 de 10" é 10 menos as linhas de uso.
CREATE TABLE usos_do_pacote (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    extra_id   uuid        NOT NULL REFERENCES extras (id) ON DELETE CASCADE,
    usado_em   date        NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    -- Uma sessão do pacote por dia: o mesmo dia marcado duas vezes é dedo duplo, não duas
    -- sessões. Quem treina duas vezes no mesmo dia é caso raro o bastante para o personal
    -- resolver vendendo outro pacote.
    CONSTRAINT usos_um_por_dia UNIQUE (extra_id, usado_em)
);

CREATE INDEX usos_do_pacote_extra ON usos_do_pacote (extra_id);

-- +goose Down

DROP TABLE usos_do_pacote;
DROP INDEX extras_por_produto;
ALTER TABLE extras DROP COLUMN taxa_cents;
ALTER TABLE extras DROP COLUMN produto_id;
DROP TABLE produtos;
