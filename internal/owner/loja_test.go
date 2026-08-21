package owner

import (
	"context"
	"testing"

	"github.com/Vitorepf/linkgym-api/internal/seed"
)

// A ALUNA LEVANTA A MÃO e o personal fecha. Ela não paga nada aqui, não escolhe forma de
// pagamento e não recebe cobrança — o verbo dela é "quero", e o que sai disso é um nome na
// tela dele com o produto do lado.
func TestAlunaPedeEOPersonalFecha(t *testing.T) {
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
	op, err := svc.Operacao(ctx, fred)
	if err != nil {
		t.Fatal(err)
	}
	aluna := op.EmAberto[0].PersonID

	loja, err := svc.LojaDoAluno(ctx, aluna)
	if err != nil {
		t.Fatal(err)
	}
	if len(loja) != 1 || loja[0].Nome != "Whey protein" || loja[0].JaPedi {
		t.Fatalf("loja %+v", loja)
	}

	if err := svc.QueroEsse(ctx, aluna, whey.ID); err != nil {
		t.Fatal(err)
	}
	// Dedo duplo não é dois wheys.
	if err := svc.QueroEsse(ctx, aluna, whey.ID); err != nil {
		t.Fatalf("pedir duas vezes deu erro: %v", err)
	}

	comPedido, err := svc.LojaDoAluno(ctx, aluna)
	if err != nil {
		t.Fatal(err)
	}
	if !comPedido[0].JaPedi {
		t.Fatal("pediu e a loja dela nao sabe")
	}

	// Do lado dele: um nome, com o produto, esperando o dedo.
	dele, err := svc.Operacao(ctx, fred)
	if err != nil {
		t.Fatal(err)
	}
	var achou bool
	for _, e := range dele.AEntregar {
		if e.ProdutoID != nil && *e.ProdutoID == whey.ID {
			achou = true
			if e.PersonID != aluna || e.ValorCents != 21900 {
				t.Fatalf("pedido chegou errado: %+v", e)
			}
		}
	}
	if !achou {
		t.Fatalf("o pedido dela nao chegou nele: %+v", dele.AEntregar)
	}
	// Só UMA linha, mesmo com dois toques.
	var n int
	if err := database.QueryRow(
		`SELECT count(*) FROM extras WHERE produto_id = $1`, whey.ID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("dois toques gravaram %d linhas", n)
	}

	// Produto pausado some da loja dela.
	if err := svc.PausarProduto(ctx, fred, whey.ID, false); err != nil {
		t.Fatal(err)
	}
	pausado, err := svc.LojaDoAluno(ctx, aluna)
	if err != nil {
		t.Fatal(err)
	}
	if len(pausado) != 0 {
		t.Fatalf("produto pausado continua na loja dela: %+v", pausado)
	}
	limparCatalogo(t, database, studio)
}

// A loja de um time não aparece para aluno de outro, e pedir produto alheio não grava.
func TestLojaNaoVazaEntreTimes(t *testing.T) {
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
	// A aluna do outro estúdio (criada pelo helper de isolamento).
	_ = bondDeOutroEstudio(t, database, fred)
	var deOutro string
	if err := database.QueryRow(
		`SELECT id::text FROM people WHERE phone = '+5511999990002'`).Scan(&deOutro); err != nil {
		t.Fatal(err)
	}
	loja, err := svc.LojaDoAluno(ctx, deOutro)
	if err != nil {
		t.Fatal(err)
	}
	if len(loja) != 0 {
		t.Fatalf("a loja do Fred apareceu para aluna de outro time: %+v", loja)
	}
	if err := svc.QueroEsse(ctx, deOutro, p.ID); err != ErrNotFound {
		t.Fatalf("aluna de outro time pediu produto do Fred: %v", err)
	}
	limparCatalogo(t, database, studio)
}

// A ALUNA ASSINA. Para um produto que repete todo mês, "quero" significa ASSINAR — e criar
// uma venda avulsa daria a ela uma marmita única que nunca repetiria, que é exatamente o
// defeito que o tipo `assinatura` existe para consertar.
func TestAlunaAssinaEmVezDePedirUmaVez(t *testing.T) {
	database := openSeeded(t)
	svc := New(database, nil)
	ctx := context.Background()
	fred := personIDByPhone(t, database, seed.PhoneFred)
	studio := studioDoFred(t, database, fred)
	limparCatalogo(t, database, studio)
	t.Cleanup(func() { limparCatalogo(t, database, studio) })

	marmita, err := svc.CriarProduto(ctx, fred, "assinatura", "Marmita fitness", 89000, nil)
	if err != nil {
		t.Fatal(err)
	}
	op, err := svc.Operacao(ctx, fred)
	if err != nil {
		t.Fatal(err)
	}
	aluna := op.EmAberto[0].PersonID

	if err := svc.QueroEsse(ctx, aluna, marmita.ID); err != nil {
		t.Fatal(err)
	}
	// Virou ASSINATURA, não venda avulsa.
	var assinaturas, avulsas int
	if err := database.QueryRow(
		`SELECT count(*) FROM assinaturas WHERE produto_id = $1 AND ate IS NULL`,
		marmita.ID).Scan(&assinaturas); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(
		`SELECT count(*) FROM extras WHERE produto_id = $1`, marmita.ID).Scan(&avulsas); err != nil {
		t.Fatal(err)
	}
	if assinaturas != 1 {
		t.Fatalf("a aluna pediu a marmita e nao virou assinatura: %d", assinaturas)
	}
	if avulsas != 0 {
		t.Fatalf("virou %d venda(s) avulsa(s) — a marmita nunca repetiria", avulsas)
	}

	// Do lado dele: ela aparece devendo o mês, com o nome e o produto.
	dele, err := svc.Operacao(ctx, fred)
	if err != nil {
		t.Fatal(err)
	}
	if len(dele.Assinaturas) != 1 || dele.Assinaturas[0].PersonID != aluna {
		t.Fatalf("a assinatura dela nao chegou nele: %+v", dele.Assinaturas)
	}
	if dele.Assinaturas[0].MesesAbertos != 1 || dele.Assinaturas[0].DevidoCents != 89000 {
		t.Fatalf("assinatura %+v", dele.Assinaturas[0])
	}

	// Tocar duas vezes é a mesma verdade, não duas marmitas.
	if err := svc.QueroEsse(ctx, aluna, marmita.ID); err != nil {
		t.Fatalf("pedir de novo virou erro: %v", err)
	}
	if err := database.QueryRow(
		`SELECT count(*) FROM assinaturas WHERE produto_id = $1`, marmita.ID).Scan(&assinaturas); err != nil {
		t.Fatal(err)
	}
	if assinaturas != 1 {
		t.Fatalf("dois toques criaram %d assinaturas", assinaturas)
	}

	// E a loja dela sabe: o botão vira "Pedido".
	loja, err := svc.LojaDoAluno(ctx, aluna)
	if err != nil {
		t.Fatal(err)
	}
	if len(loja) != 1 || !loja[0].JaPedi {
		t.Fatalf("assinou e a loja dela nao sabe: %+v", loja)
	}
}

// A PORTA TEM DUAS MÃOS. "Assinar" criava dívida mensal recorrente com UM toque e não saía
// por lugar nenhum: nem pela tela dela, nem pela dele. O limite do desfazer é o dinheiro —
// enquanto nada foi recebido ela desfaz; depois do primeiro mês recebido, quem encerra é ele.
func TestAlunaDesisteEnquantoNadaFoiRecebido(t *testing.T) {
	database := openSeeded(t)
	svc := New(database, nil)
	ctx := context.Background()
	fred := personIDByPhone(t, database, seed.PhoneFred)
	studio := studioDoFred(t, database, fred)
	limparCatalogo(t, database, studio)
	t.Cleanup(func() { limparCatalogo(t, database, studio) })

	marmita, err := svc.CriarProduto(ctx, fred, "assinatura", "Marmita fitness", 89000, nil)
	if err != nil {
		t.Fatal(err)
	}
	whey, err := svc.CriarProduto(ctx, fred, "fisico", "Whey", 21900, nil)
	if err != nil {
		t.Fatal(err)
	}
	op, err := svc.Operacao(ctx, fred)
	if err != nil {
		t.Fatal(err)
	}
	aluna := op.EmAberto[0].PersonID

	conta := func(q string, args ...any) int {
		var n int
		if err := database.QueryRow(q, args...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	assinaturasVivas := func() int {
		return conta(`SELECT count(*) FROM assinaturas WHERE produto_id = $1 AND ate IS NULL`, marmita.ID)
	}

	// Assina e desiste: some inteira, sem deixar historia de uma assinatura que nunca houve.
	if err := svc.QueroEsse(ctx, aluna, marmita.ID); err != nil {
		t.Fatal(err)
	}
	if assinaturasVivas() != 1 {
		t.Fatal("nao assinou")
	}
	if err := svc.DesistirDoPedido(ctx, aluna, marmita.ID); err != nil {
		t.Fatal(err)
	}
	if n := conta(`SELECT count(*) FROM assinaturas WHERE produto_id = $1`, marmita.ID); n != 0 {
		t.Fatalf("desistiu e sobraram %d linhas de assinatura", n)
	}
	// Desistir de novo e a mesma verdade.
	if err := svc.DesistirDoPedido(ctx, aluna, marmita.ID); err != nil {
		t.Fatalf("desistir duas vezes virou erro: %v", err)
	}

	// A venda avulsa segue a mesma lei.
	if err := svc.QueroEsse(ctx, aluna, whey.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.DesistirDoPedido(ctx, aluna, whey.ID); err != nil {
		t.Fatal(err)
	}
	if n := conta(`SELECT count(*) FROM extras WHERE produto_id = $1`, whey.ID); n != 0 {
		t.Fatalf("desistiu do whey e sobraram %d linhas", n)
	}

	// DEPOIS DO DINHEIRO, NAO. Assina, ele recebe o mes, e o desfazer dela para de valer:
	// apagar fato de caixa pela tela de quem paga seria o app reescrevendo o livro dele.
	if err := svc.QueroEsse(ctx, aluna, marmita.ID); err != nil {
		t.Fatal(err)
	}
	dele, err := svc.Operacao(ctx, fred)
	if err != nil {
		t.Fatal(err)
	}
	if len(dele.Assinaturas) != 1 {
		t.Fatalf("assinaturas dele: %+v", dele.Assinaturas)
	}
	if err := svc.ReceberAssinatura(ctx, fred, dele.Assinaturas[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.DesistirDoPedido(ctx, aluna, marmita.ID); err != nil {
		t.Fatal(err)
	}
	if assinaturasVivas() != 1 {
		t.Fatal("ela apagou uma assinatura que ja tinha mes recebido")
	}

	// E ai quem encerra e ELE, e o mes recebido continua sendo fato.
	if err := svc.CancelarAssinatura(ctx, fred, dele.Assinaturas[0].ID); err != nil {
		t.Fatal(err)
	}
	if assinaturasVivas() != 0 {
		t.Fatal("ele encerrou e a assinatura continua viva")
	}
	if n := conta(`SELECT count(*) FROM extras WHERE produto_id = $1 AND recebida_em IS NOT NULL`, marmita.ID); n != 1 {
		t.Fatalf("o mes recebido sumiu junto: %d", n)
	}
}
