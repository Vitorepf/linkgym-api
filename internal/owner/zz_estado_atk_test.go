package owner

import (
	"context"
	"testing"

	"github.com/Vitorepf/linkgym-api/internal/seed"
)

// (A) O ALUNO QUE ENTROU HOJE JA NASCE VENCIDO.
func TestAtkNovoAlunoNasceVencido(t *testing.T) {
	database := openSeeded(t)
	svc := New(database, nil)
	ctx := context.Background()
	fred := personIDByPhone(t, database, seed.PhoneFred)
	studioID, err := svc.ownerTime(ctx, fred)
	if err != nil {
		t.Fatal(err)
	}
	hoje := svc.now().Day()
	if hoje < 3 {
		t.Skip("precisa de dia >= 3 no mes civil")
	}
	var bondID string
	if err := database.QueryRowContext(ctx, `
		WITH p AS (INSERT INTO people (phone, name) VALUES ('+5511977778801','Nova Aluna') RETURNING id)
		INSERT INTO bonds (person_id, studio_id, role, status)
		SELECT p.id, $1, 'student', 'active' FROM p RETURNING id`, studioID).Scan(&bondID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = database.Exec(`DELETE FROM people WHERE phone='+5511977778801'`) })
	// O personal acabou de digitar o combinado: R$350, todo dia 1.
	if err := svc.DefinirMensalidade(ctx, fred, bondID, 35000, 1); err != nil {
		t.Fatal(err)
	}
	op, err := svc.Operacao(ctx, fred)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range op.EmAberto {
		if it.BondID == bondID {
			t.Logf("NOVA ALUNA (vinculo criado agora): vencido_ha=%d  -> a tela escreve \"venceu ha %d dias\"", it.VencidoHa, it.VencidoHa)
			if it.VencidoHa > 0 {
				t.Errorf("REPRODUZIDO: aluna cadastrada hoje aparece vencida ha %d dias e entra em vencido_cents", it.VencidoHa)
			}
			return
		}
	}
	t.Fatal("nao apareceu em em_aberto")
}

// (B) O EXTRA EM ABERTO SOME QUANDO O VINCULO PAUSA/ENCERRA.
func TestAtkExtraSomeAoEncerrar(t *testing.T) {
	database := openSeeded(t)
	svc := New(database, nil)
	ctx := context.Background()
	fred := personIDByPhone(t, database, seed.PhoneFred)
	studioID, err := svc.ownerTime(ctx, fred)
	if err != nil {
		t.Fatal(err)
	}
	var bondID string
	if err := database.QueryRowContext(ctx, `
		SELECT b.id::text FROM bonds b JOIN people p ON p.id=b.person_id
		WHERE b.studio_id=$1 AND b.role='student' AND b.status='active' LIMIT 1`, studioID).Scan(&bondID); err != nil {
		t.Fatal(err)
	}
	ex, err := svc.CriarExtra(ctx, fred, bondID, "Avaliacao fisica", 15000)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = database.Exec(`DELETE FROM extras WHERE id=$1`, ex.ID)
		_, _ = database.Exec(`UPDATE bonds SET status='active' WHERE id=$1`, bondID)
	})
	abertos, _ := svc.ExtrasAbertos(ctx, studioID)
	t.Logf("antes de encerrar: %d extras em aberto", len(abertos))

	if err := svc.MudarEstadoDoVinculo(ctx, fred, bondID, VinculoEncerrado); err != nil {
		t.Fatal(err)
	}
	depois, _ := svc.ExtrasAbertos(ctx, studioID)
	for _, c := range depois {
		if c.ID == ex.ID {
			return
		}
	}
	t.Errorf("REPRODUZIDO: R$ 150,00 vendidos e nao recebidos sumiram da tela ao encerrar o vinculo (%d -> %d), sem serem recebidos nem cancelados", len(abertos), len(depois))

	// E o desfazer? Reativar traz de volta?
	if err := svc.MudarEstadoDoVinculo(ctx, fred, bondID, VinculoAtivo); err != nil {
		t.Fatal(err)
	}
	volta, _ := svc.ExtrasAbertos(ctx, studioID)
	t.Logf("depois de reativar: %d", len(volta))
}
