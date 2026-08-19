package owner

import (
	"context"
	"testing"

	"github.com/Vitorepf/linkgym-api/internal/seed"
)

// O DEFEITO CUMULATIVO, em teste: quem sai tem que sair de TODOS os números. Antes disto
// não havia como registrar uma saída, e o estúdio do personal engordava para sempre —
// alunos que foram embora continuavam contando, faturando e devendo.
func TestEncerrarTiraDeTodosOsNumeros(t *testing.T) {
	database := openSeeded(t)
	svc := New(database, nil)
	ctx := context.Background()
	fred := personIDByPhone(t, database, seed.PhoneFred)

	antes, err := svc.Operacao(ctx, fred)
	if err != nil {
		t.Fatal(err)
	}
	if len(antes.EmAberto) == 0 {
		t.Fatal("preciso de alguem em aberto para encerrar")
	}
	saiu := antes.EmAberto[0]

	if err := svc.MudarEstadoDoVinculo(ctx, fred, saiu.BondID, VinculoEncerrado); err != nil {
		t.Fatal(err)
	}
	depois, err := svc.Operacao(ctx, fred)
	if err != nil {
		t.Fatal(err)
	}
	if depois.StudentCount != antes.StudentCount-1 {
		t.Fatalf("alunos %d, esperava %d", depois.StudentCount, antes.StudentCount-1)
	}
	if depois.ReceitaCents != antes.ReceitaCents-saiu.AmountCents {
		t.Fatalf("o combinado nao encolheu: %d -> %d", antes.ReceitaCents, depois.ReceitaCents)
	}
	for _, it := range depois.EmAberto {
		if it.BondID == saiu.BondID {
			t.Fatal("encerrado e continua devendo")
		}
	}
	for _, r := range depois.Risco {
		if r.BondID == saiu.BondID {
			t.Fatal("encerrado e continua na fila de risco")
		}
	}

	// PAUSAR também tira das contas do mês: quem viajou dois meses não deve nada.
	if err := svc.MudarEstadoDoVinculo(ctx, fred, saiu.BondID, VinculoPausado); err != nil {
		t.Fatal(err)
	}
	pausado, err := svc.Operacao(ctx, fred)
	if err != nil {
		t.Fatal(err)
	}
	if pausado.StudentCount != antes.StudentCount-1 {
		t.Fatalf("pausado ainda conta: %d", pausado.StudentCount)
	}

	// E VOLTA: encerrar por engano não pode ser porta de mão única, e "voltei a treinar"
	// é comum. O histórico da pessoa continua inteiro.
	if err := svc.MudarEstadoDoVinculo(ctx, fred, saiu.BondID, VinculoAtivo); err != nil {
		t.Fatal(err)
	}
	voltou, err := svc.Operacao(ctx, fred)
	if err != nil {
		t.Fatal(err)
	}
	if voltou.StudentCount != antes.StudentCount {
		t.Fatalf("reativou e o total nao voltou: %d != %d", voltou.StudentCount, antes.StudentCount)
	}
	if voltou.ReceitaCents != antes.ReceitaCents {
		t.Fatalf("reativou e o combinado nao voltou: %d != %d", voltou.ReceitaCents, antes.ReceitaCents)
	}
}

func TestEstadoDoVinculoRecusaOInvalido(t *testing.T) {
	database := openSeeded(t)
	svc := New(database, nil)
	ctx := context.Background()
	fred := personIDByPhone(t, database, seed.PhoneFred)
	op, err := svc.Operacao(ctx, fred)
	if err != nil {
		t.Fatal(err)
	}
	bond := op.EmAberto[0].BondID

	for _, ruim := range []EstadoDoVinculo{"", "invited", "morto", "ACTIVE"} {
		if err := svc.MudarEstadoDoVinculo(ctx, fred, bond, ruim); err != ErrInvalid {
			t.Fatalf("estado %q aceito: %v", ruim, err)
		}
	}
	// Vínculo de outro estúdio não existe para este personal.
	outro := bondDeOutroEstudio(t, database, fred)
	if err := svc.MudarEstadoDoVinculo(ctx, fred, outro, VinculoEncerrado); err != ErrNotFound {
		t.Fatalf("encerrou vinculo de outro estudio: %v", err)
	}
	var status string
	if err := database.QueryRow(`SELECT status FROM bonds WHERE id = $1`, outro).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "active" {
		t.Fatalf("o vinculo do outro estudio virou %q", status)
	}
}
