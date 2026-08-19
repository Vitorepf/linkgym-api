package owner

import (
	"context"
	"strings"
	"testing"

	"github.com/Vitorepf/linkgym-api/internal/seed"
)

// O convite é a única porta do aluno, e é o primeiro artefato da marca do personal que sai
// do app. Três propriedades sustentam essa promessa e as três são testadas aqui: o código
// carrega o nome do time, ele é legível em voz alta, e o mesmo aluno recebe sempre o mesmo
// — porque o personal que toca duas vezes no botão não pode mandar dois links diferentes
// para a mesma pessoa.
func TestConviteCarregaAMarcaEEIdempotente(t *testing.T) {
	database := openSeeded(t)
	svc := New(database, nil)
	fred := personIDByPhone(t, database, seed.PhoneFred)
	ctx := context.Background()

	um, err := svc.CriarConvite(ctx, fred, "+5511977776666")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(um.Code, "FRE-") {
		t.Fatalf("código %q não carrega o nome do time", um.Code)
	}
	// Alfabeto sem 0/O e sem 1/I/L: o código é lido em voz alta na porta da academia.
	for _, r := range strings.TrimPrefix(um.Code, "FRE-") {
		if strings.ContainsRune("01OIL", r) {
			t.Fatalf("código %q tem glifo ambíguo %q", um.Code, r)
		}
	}
	if !um.ExpiresAt.After(svc.now()) {
		t.Fatalf("convite já nasce expirado: %s", um.ExpiresAt)
	}

	dois, err := svc.CriarConvite(ctx, fred, "+5511977776666")
	if err != nil {
		t.Fatal(err)
	}
	if dois.Code != um.Code {
		t.Fatalf("o mesmo telefone recebeu dois códigos: %q e %q", um.Code, dois.Code)
	}

	// Sem telefone é convite ABERTO — serve para quem for, e por isso não pode ser
	// deduplicado contra o do aluno nomeado.
	aberto, err := svc.CriarConvite(ctx, fred, "")
	if err != nil {
		t.Fatal(err)
	}
	if aberto.Code == um.Code {
		t.Fatalf("convite aberto reaproveitou o código de um aluno nomeado: %q", aberto.Code)
	}
}

// Quem não é dono de time não abre porta nenhuma.
func TestConviteExigeDono(t *testing.T) {
	database := openSeeded(t)
	svc := New(database, nil)
	aluno := personIDByPhone(t, database, seed.PhoneVitor)

	if _, err := svc.CriarConvite(context.Background(), aluno, ""); err == nil {
		t.Fatal("aluno criou convite; esperava recusa")
	}
}
