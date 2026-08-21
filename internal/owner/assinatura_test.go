package owner

import (
	"context"
	"testing"
	"time"

	"github.com/Vitorepf/linkgym-api/internal/seed"
)

// A ASSINATURA REPETE — e essa é a frase inteira do teste. O tipo dizia "repete todo mês" no
// próprio cardápio e nada repetia: vender a marmita criava UMA linha, uma vez, e no mês
// seguinte não havia nada.
func TestAssinaturaRepeteTodoMes(t *testing.T) {
	database := openSeeded(t)
	ctx := context.Background()
	fred := personIDByPhone(t, database, seed.PhoneFred)
	studio := studioDoFred(t, database, fred)
	limparCatalogo(t, database, studio)
	t.Cleanup(func() { limparCatalogo(t, database, studio) })

	// Março: o personal cria a marmita e a aluna assina.
	marco := time.Date(2026, 3, 10, 9, 0, 0, 0, time.UTC)
	svcMarco := New(database, func() time.Time { return marco })
	marmita, err := svcMarco.CriarProduto(ctx, fred, "assinatura", "Marmita fitness", 89000, nil)
	if err != nil {
		t.Fatal(err)
	}
	// O vínculo vem do banco, não da Operação: com o relógio em MARÇO e o seed criado em
	// agosto, a lista de em aberto é vazia por construção (a série de competências andaria
	// para trás). O cenário desta prova é a assinatura, não a mensalidade.
	var bond string
	if err := database.QueryRow(`
		SELECT b.id::text FROM bonds b
		WHERE b.studio_id = $1 AND b.role = 'student' AND b.status = 'active'
		ORDER BY b.created_at LIMIT 1`, studio).Scan(&bond); err != nil {
		t.Fatal(err)
	}
	// O vínculo precisa existir antes da assinatura.
	if _, err := database.Exec(
		`UPDATE bonds SET created_at = '2026-02-01' WHERE id = $1`, bond); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = database.Exec(`UPDATE bonds SET created_at = now() WHERE id = $1`, bond)
	})

	// VENDER uma assinatura como avulsa e recusado: assinatura nao e venda.
	if _, err := svcMarco.VenderProduto(ctx, fred, marmita.ID, bond); err != ErrInvalid {
		t.Fatalf("vendeu assinatura como avulsa: %v", err)
	}

	a, err := svcMarco.Assinar(ctx, fred, marmita.ID, bond)
	if err != nil {
		t.Fatal(err)
	}
	if a == nil || a.ValorCents != 89000 || a.Desde != "2026-03-01" {
		t.Fatalf("assinatura %+v", a)
	}
	// Assinar duas vezes e a mesma verdade, nao duas marmitas.
	if _, err := svcMarco.Assinar(ctx, fred, marmita.ID, bond); err != nil {
		t.Fatalf("assinar de novo virou erro: %v", err)
	}
	var quantas int
	if err := database.QueryRow(
		`SELECT count(*) FROM assinaturas WHERE bond_id = $1 AND produto_id = $2`,
		bond, marmita.ID).Scan(&quantas); err != nil {
		t.Fatal(err)
	}
	if quantas != 1 {
		t.Fatalf("dois toques criaram %d assinaturas", quantas)
	}

	// Em MARÇO ela deve um mês.
	emMarco, err := svcMarco.Operacao(ctx, fred)
	if err != nil {
		t.Fatal(err)
	}
	if len(emMarco.Assinaturas) != 1 || emMarco.Assinaturas[0].MesesAbertos != 1 {
		t.Fatalf("marco: %+v", emMarco.Assinaturas)
	}

	// EM JUNHO, sem ninguém rodar nada, ela deve QUATRO — março, abril, maio e junho. É o
	// ponto inteiro: a recorrência é derivada, não gerada, então não existe o dia em que o
	// processo do dia 1º falhou e a turma ficou sem cobrança.
	junho := time.Date(2026, 6, 15, 9, 0, 0, 0, time.UTC)
	svcJunho := New(database, func() time.Time { return junho })
	emJunho, err := svcJunho.Operacao(ctx, fred)
	if err != nil {
		t.Fatal(err)
	}
	if len(emJunho.Assinaturas) != 1 {
		t.Fatalf("junho: %+v", emJunho.Assinaturas)
	}
	if emJunho.Assinaturas[0].MesesAbertos != 4 {
		t.Fatalf("junho: %d meses abertos, esperava 4", emJunho.Assinaturas[0].MesesAbertos)
	}
	if emJunho.Assinaturas[0].DevidoCents != 4*89000 {
		t.Fatalf("devido %d", emJunho.Assinaturas[0].DevidoCents)
	}

	// RECEBER quita a MAIS ANTIGA, uma por vez, como na mensalidade.
	if err := svcJunho.ReceberAssinatura(ctx, fred, emJunho.Assinaturas[0].ID); err != nil {
		t.Fatal(err)
	}
	depois, err := svcJunho.Operacao(ctx, fred)
	if err != nil {
		t.Fatal(err)
	}
	if depois.Assinaturas[0].MesesAbertos != 3 {
		t.Fatalf("depois de receber: %d", depois.Assinaturas[0].MesesAbertos)
	}
	if depois.Assinaturas[0].PrimeiraAberta != "2026-04-01" {
		t.Fatalf("quitou a competencia errada: a mais antiga agora e %s",
			depois.Assinaturas[0].PrimeiraAberta)
	}
	// O VALOR CONGELOU: subir a marmita nao reescreve o que ela combinou em marco.
	if _, err := database.Exec(
		`UPDATE produtos SET preco_cents = 95000 WHERE id = $1`, marmita.ID); err != nil {
		t.Fatal(err)
	}
	comPrecoNovo, err := svcJunho.Operacao(ctx, fred)
	if err != nil {
		t.Fatal(err)
	}
	if comPrecoNovo.Assinaturas[0].ValorCents != 89000 {
		t.Fatalf("subir o preco reescreveu a assinatura: %d", comPrecoNovo.Assinaturas[0].ValorCents)
	}

	// CANCELAR grava uma data e nao apaga: o que ela pagou continua sendo fato, e o mes
	// corrente continua devido — cancelar no dia 15 nao devolve o mes.
	if err := svcJunho.CancelarAssinatura(ctx, fred, emJunho.Assinaturas[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := svcJunho.CancelarAssinatura(ctx, fred, emJunho.Assinaturas[0].ID); err != nil {
		t.Fatalf("cancelar duas vezes virou erro: %v", err)
	}
	// Em JULHO ela nao deve mais nada novo: a janela parou em junho.
	julho := time.Date(2026, 7, 15, 9, 0, 0, 0, time.UTC)
	emJulho, err := New(database, func() time.Time { return julho }).Operacao(ctx, fred)
	if err != nil {
		t.Fatal(err)
	}
	if len(emJulho.Assinaturas) != 1 || emJulho.Assinaturas[0].MesesAbertos != 3 {
		t.Fatalf("julho depois do cancelamento: %+v", emJulho.Assinaturas)
	}
	// E o historico ficou: a linha de marco recebida continua la.
	var recebidas int
	if err := database.QueryRow(
		`SELECT count(*) FROM extras WHERE produto_id = $1 AND recebida_em IS NOT NULL`,
		marmita.ID).Scan(&recebidas); err != nil {
		t.Fatal(err)
	}
	if recebidas != 1 {
		t.Fatalf("o historico da assinatura sumiu: %d linhas recebidas", recebidas)
	}
}

// Nao vaza entre times.
func TestAssinaturaNaoVazaEntreTimes(t *testing.T) {
	database := openSeeded(t)
	svc := New(database, nil)
	ctx := context.Background()
	fred := personIDByPhone(t, database, seed.PhoneFred)
	studio := studioDoFred(t, database, fred)
	limparCatalogo(t, database, studio)
	t.Cleanup(func() { limparCatalogo(t, database, studio) })

	p, err := svc.CriarProduto(ctx, fred, "assinatura", "Consultoria online", 30000, nil)
	if err != nil {
		t.Fatal(err)
	}
	outro := bondDeOutroEstudio(t, database, fred)
	if _, err := svc.Assinar(ctx, fred, p.ID, outro); err != ErrNotFound {
		t.Fatalf("assinou vinculo de outro time: %v", err)
	}
	if err := svc.ReceberAssinatura(ctx, fred, "00000000-0000-0000-0000-000000000000"); err != ErrNotFound {
		t.Fatalf("recebeu assinatura inexistente: %v", err)
	}
}
