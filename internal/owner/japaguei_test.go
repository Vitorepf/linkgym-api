package owner

import (
	"context"
	"testing"

	"github.com/Vitorepf/linkgym-api/internal/seed"
)

// O CIRCUITO que faltava: a aluna diz, o personal vê o nome no topo, e o dedo dele confirma.
// Nada do que ela diz muda um centavo — é a diferença entre dar voz e dar caneta.
func TestOAlunoDizEOPersonalConfirma(t *testing.T) {
	database := openSeeded(t)
	svc := New(database, nil)
	ctx := context.Background()
	fred := personIDByPhone(t, database, seed.PhoneFred)

	antes, err := svc.Operacao(ctx, fred)
	if err != nil {
		t.Fatal(err)
	}
	if len(antes.EmAberto) < 2 {
		t.Fatal("preciso de dois em aberto para provar a ordem")
	}
	// A ÚLTIMA da lista: se ela subir para o topo depois de falar, a ordenação é a que o
	// personal precisa (conferir vem antes de lembrar).
	ultima := antes.EmAberto[len(antes.EmAberto)-1]

	// O que ela vê antes de falar: o combinado, e nenhuma palavra sobre atraso.
	dela, err := svc.MensalidadeDoAluno(ctx, ultima.PersonID)
	if err != nil {
		t.Fatal(err)
	}
	if dela.ValorCents == nil || *dela.ValorCents != ultima.AmountCents {
		t.Fatalf("o combinado dela nao bate: %+v", dela)
	}
	if dela.JaDisse || dela.Recebido {
		t.Fatalf("estado inicial errado: %+v", dela)
	}

	if err := svc.DizerQueJaPagou(ctx, ultima.PersonID); err != nil {
		t.Fatal(err)
	}
	// Idempotente: repetir é a mesma verdade.
	if err := svc.DizerQueJaPagou(ctx, ultima.PersonID); err != nil {
		t.Fatalf("dizer duas vezes deu erro: %v", err)
	}

	depois, err := svc.Operacao(ctx, fred)
	if err != nil {
		t.Fatal(err)
	}
	// NADA de dinheiro mudou. Ela falou; ela não marcou.
	if depois.RecebidoCents != antes.RecebidoCents {
		t.Fatalf("a fala dela mexeu no recebido: %d -> %d", antes.RecebidoCents, depois.RecebidoCents)
	}
	if len(depois.EmAberto) != len(antes.EmAberto) {
		t.Fatalf("a fala dela mexeu na lista: %d -> %d", len(antes.EmAberto), len(depois.EmAberto))
	}
	// E ela subiu para o topo, com a marca.
	if depois.EmAberto[0].PersonID != ultima.PersonID {
		t.Fatalf("quem disse que pagou nao subiu: topo e %s", depois.EmAberto[0].Name)
	}
	if !depois.EmAberto[0].DizQueJaPagou {
		t.Fatal("subiu sem a marca")
	}

	// O dedo do personal fecha o circuito, e aí sim o dinheiro anda.
	if err := svc.PagarMensalidade(ctx, fred, ultima.BondID); err != nil {
		t.Fatal(err)
	}
	fechado, err := svc.Operacao(ctx, fred)
	if err != nil {
		t.Fatal(err)
	}
	if fechado.RecebidoCents != antes.RecebidoCents+ultima.AmountCents {
		t.Fatalf("o dedo dele nao andou: %d", fechado.RecebidoCents)
	}
	// Do lado dela, o mês fecha e não há mais o que dizer.
	final, err := svc.MensalidadeDoAluno(ctx, ultima.PersonID)
	if err != nil {
		t.Fatal(err)
	}
	if !final.Recebido {
		t.Fatalf("o personal marcou e ela nao sabe: %+v", final)
	}

	if _, err := database.Exec(`DELETE FROM ja_paguei WHERE bond_id = $1`, ultima.BondID); err != nil {
		t.Fatal(err)
	}
	if err := svc.DesfazerPagamento(ctx, fred, ultima.BondID); err != nil {
		t.Fatal(err)
	}
}

// A LEI: o payload do aluno não tem como dizer que ele está atrasado. Não existe o campo, e
// é por isso que nenhuma tela futura vai conseguir desenhar um.
func TestOAlunoNuncaRecebeEstadoDeAtraso(t *testing.T) {
	database := openSeeded(t)
	svc := New(database, nil)
	fred := personIDByPhone(t, database, seed.PhoneFred)
	op, err := svc.Operacao(context.Background(), fred)
	if err != nil {
		t.Fatal(err)
	}
	// O Jose está em aberto no seed, e vencido.
	var alvo string
	for _, it := range op.EmAberto {
		if it.VencidoHa > 0 {
			alvo = it.PersonID
		}
	}
	if alvo == "" {
		t.Skip("ninguem vencido no seed hoje")
	}
	dela, err := svc.MensalidadeDoAluno(context.Background(), alvo)
	if err != nil {
		t.Fatal(err)
	}
	// O tipo tem quatro campos e nenhum deles é "vencido", "atrasado" ou "em aberto".
	// Se alguém acrescentar um, este teste não quebra — mas o comentário e a lei ficam,
	// e o revisor tem onde apoiar a recusa.
	if dela.ValorCents == nil {
		t.Fatal("sem combinado no alvo")
	}
	if dela.Recebido {
		t.Fatal("em aberto e o payload dela diz recebido")
	}
}
