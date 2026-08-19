package owner

import (
	"context"
	"strings"
	"testing"

	"github.com/Vitorepf/linkgym-api/internal/seed"
)

// O ciclo do que ele vende fora da mensalidade: cria com DOIS campos, aparece na Operação
// com a descrição na linha, recebe, some — e desfazer devolve.
func TestExtraAvulsaDoInicioAoFim(t *testing.T) {
	database := openSeeded(t)
	svc := New(database, nil)
	ctx := context.Background()
	fred := personIDByPhone(t, database, seed.PhoneFred)

	op, err := svc.Operacao(ctx, fred)
	if err != nil {
		t.Fatal(err)
	}
	if len(op.AEntregar) != 0 {
		t.Fatalf("o seed nao deveria ter extra avulsa: %+v", op.AEntregar)
	}
	bond := op.EmAberto[0].BondID

	nova, err := svc.CriarExtra(ctx, fred, bond, "Avaliação física", 15000)
	if err != nil {
		t.Fatal(err)
	}
	if nova.Descricao != "Avaliação física" || nova.ValorCents != 15000 {
		t.Fatalf("gravou %+v", nova)
	}
	if nova.Name == "" || nova.PersonID == "" {
		t.Fatalf("extra sem a pessoa: %+v", nova)
	}

	comAvulsa, err := svc.Operacao(ctx, fred)
	if err != nil {
		t.Fatal(err)
	}
	if len(comAvulsa.AEntregar) != 1 {
		t.Fatalf("a entregar: %+v", comAvulsa.AEntregar)
	}
	if comAvulsa.AEntregar[0].Descricao != "Avaliação física" {
		t.Fatalf("a descricao nao chegou na linha: %+v", comAvulsa.AEntregar[0])
	}

	if err := svc.ReceberExtra(ctx, fred, nova.ID, false); err != nil {
		t.Fatal(err)
	}
	// Recebido não é trabalho: sai da lista.
	recebida, err := svc.Operacao(ctx, fred)
	if err != nil {
		t.Fatal(err)
	}
	if len(recebida.AEntregar) != 0 {
		t.Fatalf("recebida e continua na lista: %+v", recebida.AEntregar)
	}
	// Repetir o toque não é erro.
	if err := svc.ReceberExtra(ctx, fred, nova.ID, false); err != nil {
		t.Fatalf("receber duas vezes deu erro: %v", err)
	}
	// Desfazer devolve.
	if err := svc.ReceberExtra(ctx, fred, nova.ID, true); err != nil {
		t.Fatal(err)
	}
	voltou, err := svc.Operacao(ctx, fred)
	if err != nil {
		t.Fatal(err)
	}
	if len(voltou.AEntregar) != 1 {
		t.Fatalf("desfez e nao voltou: %+v", voltou.AEntregar)
	}

	// Limpa para não vazar cenário para os testes vizinhos.
	if _, err := database.Exec(`DELETE FROM extras WHERE id = $1`, nova.ID); err != nil {
		t.Fatal(err)
	}
}

// Recusa antes de gravar, e não vaza entre estúdios.
func TestExtraRecusaOInvalido(t *testing.T) {
	database := openSeeded(t)
	svc := New(database, nil)
	ctx := context.Background()
	fred := personIDByPhone(t, database, seed.PhoneFred)
	op, err := svc.Operacao(ctx, fred)
	if err != nil {
		t.Fatal(err)
	}
	bond := op.EmAberto[0].BondID

	casos := []struct {
		nome, desc string
		cents      int
		quer       error
	}{
		{"sem descricao", "   ", 1000, ErrInvalid},
		{"valor zero", "Whey", 0, ErrInvalid},
		{"valor negativo", "Whey", -1, ErrInvalid},
		{"descricao longa demais", strings.Repeat("x", 61), 1000, ErrInvalid},
	}
	for _, c := range casos {
		if _, err := svc.CriarExtra(ctx, fred, bond, c.desc, c.cents); err != c.quer {
			t.Fatalf("%s: veio %v", c.nome, err)
		}
	}
	// 60 caracteres cabem: o limite é inclusivo, e contado em RUNAS — "avaliação" tem
	// acento, e contar bytes cortaria a descrição do personal antes da hora.
	if _, err := svc.CriarExtra(ctx, fred, bond, strings.Repeat("é", 60), 1000); err != nil {
		t.Fatalf("60 runas deveria caber: %v", err)
	}
	if _, err := database.Exec(`DELETE FROM extras WHERE bond_id = $1`, bond); err != nil {
		t.Fatal(err)
	}

	outro := bondDeOutroEstudio(t, database, fred)
	if _, err := svc.CriarExtra(ctx, fred, outro, "Whey", 1000); err != ErrNotFound {
		t.Fatalf("criou extra em vinculo de outro estudio: %v", err)
	}
}
