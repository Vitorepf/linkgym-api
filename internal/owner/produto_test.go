package owner

import (
	"context"
	"database/sql"
	"testing"

	"github.com/Vitorepf/linkgym-api/internal/seed"
)

// O CICLO DO CATÁLOGO: cria o produto uma vez, vende para uma pessoa, e o nome e o preço
// CONGELAM na venda — subir o preço depois não reescreve o que ela pagou.
func TestProdutoDoCardapioAVenda(t *testing.T) {
	database := openSeeded(t)
	svc := New(database, nil)
	ctx := context.Background()
	fred := personIDByPhone(t, database, seed.PhoneFred)
	studio := studioDoFred(t, database, fred)
	limparCatalogo(t, database, studio)

	whey, err := svc.CriarProduto(ctx, fred, "fisico", "Whey protein", 21900, nil)
	if err != nil {
		t.Fatal(err)
	}
	if whey.Tipo != "fisico" || whey.PrecoCents != 21900 || !whey.Ativo {
		t.Fatalf("produto %+v", whey)
	}
	// Dois com o mesmo nome ativo é o personal se perdendo, não dois produtos.
	if _, err := svc.CriarProduto(ctx, fred, "fisico", "  whey PROTEIN ", 19900, nil); err != ErrInvalid {
		t.Fatalf("nome repetido aceito: %v", err)
	}

	op, err := svc.Operacao(ctx, fred)
	if err != nil {
		t.Fatal(err)
	}
	bond := op.EmAberto[0].BondID

	venda, err := svc.VenderProduto(ctx, fred, whey.ID, bond)
	if err != nil {
		t.Fatal(err)
	}
	if venda.Descricao != "Whey protein" || venda.ValorCents != 21900 {
		t.Fatalf("venda %+v", venda)
	}
	if venda.Name == "" {
		t.Fatal("venda sem a pessoa")
	}

	// Subir o preço NÃO reescreve o passado.
	if _, err := database.Exec(
		`UPDATE produtos SET preco_cents = 23900 WHERE id = $1`, whey.ID); err != nil {
		t.Fatal(err)
	}
	depois, err := svc.Operacao(ctx, fred)
	if err != nil {
		t.Fatal(err)
	}
	var achou bool
	for _, e := range depois.AEntregar {
		if e.ID == venda.ID {
			achou = true
			if e.ValorCents != 21900 {
				t.Fatalf("subir o preco reescreveu a venda: %d", e.ValorCents)
			}
		}
	}
	if !achou {
		t.Fatal("a venda nao entrou em A entregar")
	}

	// O cardápio carrega o dinheiro que cada linha fez.
	var noCardapio *Produto
	for i := range depois.Produtos {
		if depois.Produtos[i].ID == whey.ID {
			noCardapio = &depois.Produtos[i]
		}
	}
	if noCardapio == nil || noCardapio.Vendidos != 1 || noCardapio.AbertoCents != 21900 {
		t.Fatalf("cardapio %+v", noCardapio)
	}

	// PAUSAR é o estoque dele: some do cardápio de venda, e a venda antiga fica inteira.
	if err := svc.PausarProduto(ctx, fred, whey.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.VenderProduto(ctx, fred, whey.ID, bond); err != ErrNotFound {
		t.Fatalf("vendeu produto pausado: %v", err)
	}
	limparCatalogo(t, database, studio)
}

// O SALDO DO PACOTE é count(*), e ele para de contar quando acaba. Uma sessão por dia.
func TestPacoteGastaAteAcabar(t *testing.T) {
	database := openSeeded(t)
	svc := New(database, nil)
	ctx := context.Background()
	fred := personIDByPhone(t, database, seed.PhoneFred)
	studio := studioDoFred(t, database, fred)
	limparCatalogo(t, database, studio)

	tres := 3
	pacote, err := svc.CriarProduto(ctx, fred, "pacote", "Pacote de 3 sessões", 30000, &tres)
	if err != nil {
		t.Fatal(err)
	}
	// Pacote SEM número não existe: é um produto que ninguém sabe consumir.
	if _, err := svc.CriarProduto(ctx, fred, "pacote", "Pacote sem numero", 10000, nil); err != ErrInvalid {
		t.Fatalf("pacote sem sessoes aceito: %v", err)
	}
	// E o contrário: avulso não carrega saldo.
	if _, err := svc.CriarProduto(ctx, fred, "avulso", "Avulso com saldo", 10000, &tres); err != ErrInvalid {
		t.Fatalf("avulso com sessoes aceito: %v", err)
	}

	op, _ := svc.Operacao(ctx, fred)
	venda, err := svc.VenderProduto(ctx, fred, pacote.ID, op.EmAberto[0].BondID)
	if err != nil {
		t.Fatal(err)
	}
	// Pago, mas o trabalho não acabou: o pacote fica na lista enquanto tiver saldo.
	if err := svc.ReceberExtra(ctx, fred, venda.ID, false); err != nil {
		t.Fatal(err)
	}

	dias := []string{"2026-08-10", "2026-08-12", "2026-08-14"}
	for i, d := range dias {
		if err := svc.GastarSessao(ctx, fred, venda.ID, d); err != nil {
			t.Fatalf("uso %d: %v", i, err)
		}
		// O mesmo dia duas vezes é dedo duplo, não duas sessões.
		if err := svc.GastarSessao(ctx, fred, venda.ID, d); err != nil {
			t.Fatalf("uso repetido %d virou erro: %v", i, err)
		}
		atual, _ := svc.Operacao(ctx, fred)
		for _, e := range atual.AEntregar {
			if e.ID == venda.ID {
				if e.Sobram == nil || *e.Sobram != 3-(i+1) {
					t.Fatalf("depois de %d uso(s), sobram %v", i+1, e.Sobram)
				}
			}
		}
	}
	// Saldo zerado: o pacote sai da lista, porque agora o trabalho acabou de verdade.
	final, _ := svc.Operacao(ctx, fred)
	for _, e := range final.AEntregar {
		if e.ID == venda.ID {
			t.Fatalf("pacote consumido continua na lista: sobram %v", e.Sobram)
		}
	}
	// E gastar além do saldo não grava.
	if err := svc.GastarSessao(ctx, fred, venda.ID, "2026-08-20"); err != nil {
		t.Fatalf("gastar alem do saldo virou erro: %v", err)
	}
	var usos int
	if err := database.QueryRow(
		`SELECT count(*) FROM usos_do_pacote WHERE extra_id = $1`, venda.ID).Scan(&usos); err != nil {
		t.Fatal(err)
	}
	if usos != 3 {
		t.Fatalf("gravou %d usos num pacote de 3", usos)
	}
	limparCatalogo(t, database, studio)
}

func TestProdutoNaoVazaEntreTimes(t *testing.T) {
	database := openSeeded(t)
	svc := New(database, nil)
	ctx := context.Background()
	fred := personIDByPhone(t, database, seed.PhoneFred)
	studio := studioDoFred(t, database, fred)
	limparCatalogo(t, database, studio)

	p, err := svc.CriarProduto(ctx, fred, "avulso", "Avaliacao", 15000, nil)
	if err != nil {
		t.Fatal(err)
	}
	outro := bondDeOutroEstudio(t, database, fred)
	if _, err := svc.VenderProduto(ctx, fred, p.ID, outro); err != ErrNotFound {
		t.Fatalf("vendeu para vinculo de outro time: %v", err)
	}
	if err := svc.PausarProduto(ctx, fred, "00000000-0000-0000-0000-000000000000", false); err != ErrNotFound {
		t.Fatalf("pausou produto inexistente: %v", err)
	}
	limparCatalogo(t, database, studio)
}

// Cardápio limpo a cada teste: o banco é RE-SEMEADO, nunca truncado, e produto de uma
// rodada anterior falsifica o cenário desta.
func limparCatalogo(t *testing.T, database *sql.DB, studioID string) {
	t.Helper()
	// A ordem importa: `assinaturas` referencia `produtos` com ON DELETE RESTRICT — de
	// propósito, porque apagar um produto que alguém assina é apagar um combinado.
	if _, err := database.Exec(`
		DELETE FROM assinaturas WHERE produto_id IN (SELECT id FROM produtos WHERE studio_id = $1)`,
		studioID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		DELETE FROM extras WHERE produto_id IN (SELECT id FROM produtos WHERE studio_id = $1)`,
		studioID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`DELETE FROM produtos WHERE studio_id = $1`, studioID); err != nil {
		t.Fatal(err)
	}
}
