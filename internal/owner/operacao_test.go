package owner

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/Vitorepf/linkgym-api/internal/seed"
)

// A tela de operação inteira sai deste payload; se as leituras derraparem, o número que o
// personal vê é mentira. O seed é a verdade: 28 alunos, todos com mensalidade, Jose
// sempre em aberto (o seed o exclui do pagamento por nome).
func TestOperacaoLeituras(t *testing.T) {
	database := openSeeded(t)
	svc := New(database, nil)
	fred := personIDByPhone(t, database, seed.PhoneFred)

	got, err := svc.Operacao(context.Background(), fred)
	if err != nil {
		t.Fatal(err)
	}
	if got.StudentCount != 28 {
		t.Fatalf("student_count = %d, esperava 28", got.StudentCount)
	}
	if got.ComMensalidade != got.StudentCount {
		t.Fatalf("com_mensalidade = %d, esperava %d", got.ComMensalidade, got.StudentCount)
	}
	if got.ReceitaCents <= 0 || got.TicketCents <= 0 {
		t.Fatalf("receita/ticket zerados: %d / %d", got.ReceitaCents, got.TicketCents)
	}
	if got.TicketCents != got.ReceitaCents/got.ComMensalidade {
		t.Fatalf("ticket %d nao e receita/%d", got.TicketCents, got.ComMensalidade)
	}
	if len(got.EmAberto) == 0 {
		t.Fatal("em_aberto vazio: o seed deixa o Jose em aberto")
	}
	jose := false
	for _, it := range got.EmAberto {
		if it.Name == "Jose" {
			jose = true
		}
		if it.AmountCents <= 0 || it.DueDay < 1 || it.DueDay > 28 {
			t.Fatalf("item em aberto invalido: %+v", it)
		}
	}
	if !jose {
		t.Fatal("Jose nao esta em aberto")
	}
	// Rui esta a 21+ dias sem Cumprimento no seed: precisa aparecer no risco.
	rui := false
	for _, r := range got.Risco {
		if r.Name == "Rui" {
			rui = true
		}
	}
	if !rui {
		t.Fatalf("Rui fora do risco: %+v", got.Risco)
	}
}

// Pagar é fato mensal e idempotente: o segundo toque não duplica nem falha, e o aluno sai
// da lista de em aberto.
func TestPagarMensalidade(t *testing.T) {
	database := openSeeded(t)
	svc := New(database, nil)
	fred := personIDByPhone(t, database, seed.PhoneFred)

	antes, err := svc.Operacao(context.Background(), fred)
	if err != nil {
		t.Fatal(err)
	}
	if len(antes.EmAberto) == 0 {
		t.Fatal("sem em aberto para pagar")
	}
	alvo := antes.EmAberto[0]

	if err := svc.PagarMensalidade(context.Background(), fred, alvo.BondID); err != nil {
		t.Fatal(err)
	}
	if err := svc.PagarMensalidade(context.Background(), fred, alvo.BondID); err != nil {
		t.Fatalf("pagar duas vezes falhou: %v", err)
	}

	depois, err := svc.Operacao(context.Background(), fred)
	if err != nil {
		t.Fatal(err)
	}
	if len(depois.EmAberto) != len(antes.EmAberto)-1 {
		t.Fatalf("em aberto foi de %d para %d, esperava -1", len(antes.EmAberto), len(depois.EmAberto))
	}
	if err := svc.PagarMensalidade(context.Background(), fred, "00000000-0000-0000-0000-000000000000"); err != ErrNotFound {
		t.Fatalf("bond de fora devia ser ErrNotFound, veio %v", err)
	}
}

// White-label: nome e acento sao do Time; hex invalido e 400, nunca 500.
func TestPatchTime(t *testing.T) {
	database := openSeeded(t)
	svc := New(database, nil)
	fred := personIDByPhone(t, database, seed.PhoneFred)

	name := "Iron Fred"
	accent := "#22AA55"
	if err := svc.PatchTime(context.Background(), fred, TimePatch{Name: &name, Accent: &accent}); err != nil {
		t.Fatal(err)
	}
	var gotName, gotAccent string
	if err := database.QueryRow(`
		SELECT s.name, s.accent_color FROM studios s
		JOIN people o ON o.id = s.owner_person_id WHERE o.phone = $1`,
		seed.PhoneFred,
	).Scan(&gotName, &gotAccent); err != nil {
		t.Fatal(err)
	}
	if gotName != name || gotAccent != accent {
		t.Fatalf("gravou %q %q, esperava %q %q", gotName, gotAccent, name, accent)
	}

	ruim := "verde"
	if err := svc.PatchTime(context.Background(), fred, TimePatch{Accent: &ruim}); err != ErrInvalid {
		t.Fatalf("hex invalido devia ser ErrInvalid, veio %v", err)
	}
	vazio := "  "
	if err := svc.PatchTime(context.Background(), fred, TimePatch{Name: &vazio}); err != ErrInvalid {
		t.Fatalf("nome vazio devia ser ErrInvalid, veio %v", err)
	}
	if err := svc.PatchTime(context.Background(), fred, TimePatch{}); err != ErrInvalid {
		t.Fatalf("patch vazio devia ser ErrInvalid, veio %v", err)
	}

	// restaura o seed para os vizinhos de banco (os testes dividem o DATABASE_URL).
	back, backAccent := seed.TimeName, seed.AccentColor
	if err := svc.PatchTime(context.Background(), fred, TimePatch{Name: &back, Accent: &backAccent}); err != nil {
		t.Fatal(err)
	}
}

// DefinirMensalidade é a ÚNICA função que escreve o valor de que a tela inteira depende, e
// até aqui não tinha uma linha de teste: nem a validação do dia, nem o ON CONFLICT que
// corrige o combinado, nem o isolamento entre estúdios.
func TestDefinirMensalidade(t *testing.T) {
	database := openSeeded(t)
	svc := New(database, nil)
	ctx := context.Background()
	fred := personIDByPhone(t, database, seed.PhoneFred)

	op, err := svc.Operacao(ctx, fred)
	if err != nil {
		t.Fatal(err)
	}
	if len(op.EmAberto) == 0 {
		t.Fatal("preciso de um vinculo para exercitar")
	}
	bond := op.EmAberto[0].BondID

	// O dia do vencimento existe em todo mês civil: 29, 30 e 31 não são dia de vencimento,
	// são casos especiais de fevereiro. O CHECK do schema recusa, e a validação devolve
	// 400 em vez de deixar o banco devolver 500.
	for _, dia := range []int{0, 29, 31, -1} {
		if err := svc.DefinirMensalidade(ctx, fred, bond, 35000, dia); err != ErrInvalid {
			t.Fatalf("due_day %d aceito: %v", dia, err)
		}
	}
	if err := svc.DefinirMensalidade(ctx, fred, bond, -1, 5); err != ErrInvalid {
		t.Fatalf("valor negativo aceito: %v", err)
	}

	// Combinar, e depois RE-combinar: o combinado é corrigível, e a segunda escrita
	// substitui a primeira em vez de empilhar linha.
	if err := svc.DefinirMensalidade(ctx, fred, bond, 42000, 12); err != nil {
		t.Fatal(err)
	}
	if err := svc.DefinirMensalidade(ctx, fred, bond, 45000, 20); err != nil {
		t.Fatal(err)
	}
	var linhas, valor, dia int
	if err := database.QueryRow(
		`SELECT count(*), max(amount_cents), max(due_day) FROM mensalidades WHERE bond_id = $1`, bond,
	).Scan(&linhas, &valor, &dia); err != nil {
		t.Fatal(err)
	}
	if linhas != 1 || valor != 45000 || dia != 20 {
		t.Fatalf("apos re-combinar: %d linhas, %d centavos, dia %d", linhas, valor, dia)
	}

	// Vínculo de OUTRO estúdio não existe para este personal — e a resposta não revela
	// que ele existe em algum lugar.
	outro := bondDeOutroEstudio(t, database, fred)
	if err := svc.DefinirMensalidade(ctx, fred, outro, 10000, 5); err != ErrNotFound {
		t.Fatalf("bond de outro estudio aceito: %v", err)
	}
}

// O número "N sem valor combinado" agora carrega os nomes que o compõem: sem isso ele é um
// agregado que não abre porta, que é exatamente o defeito que a doutrina da casa aponta na
// tela de retenção da referência.
func TestSemCombinadoTrazOsNomes(t *testing.T) {
	database := openSeeded(t)
	svc := New(database, nil)
	ctx := context.Background()
	fred := personIDByPhone(t, database, seed.PhoneFred)

	antes, err := svc.Operacao(ctx, fred)
	if err != nil {
		t.Fatal(err)
	}
	// O seed combina com todo mundo, então a lista nasce vazia — e o agregado concorda.
	if len(antes.SemCombinado) != antes.StudentCount-antes.ComMensalidade {
		t.Fatalf("sem_combinado tem %d nomes, mas o agregado diz %d",
			len(antes.SemCombinado), antes.StudentCount-antes.ComMensalidade)
	}

	// Tira o combinado de um aluno: o nome dele TEM que aparecer, e o agregado tem que
	// andar junto. Um número que anda sem a lista é o defeito voltando.
	bond := antes.EmAberto[0].BondID
	nome := antes.EmAberto[0].Name
	if _, err := database.Exec(`DELETE FROM mensalidades WHERE bond_id = $1`, bond); err != nil {
		t.Fatal(err)
	}
	depois, err := svc.Operacao(ctx, fred)
	if err != nil {
		t.Fatal(err)
	}
	if len(depois.SemCombinado) != depois.StudentCount-depois.ComMensalidade {
		t.Fatalf("agregado %d != nomes %d",
			depois.StudentCount-depois.ComMensalidade, len(depois.SemCombinado))
	}
	achou := false
	for _, it := range depois.SemCombinado {
		if it.Name == nome {
			achou = true
			if it.BondID == "" || it.PersonID == "" {
				t.Fatalf("nome sem porta: %+v", it)
			}
		}
	}
	if !achou {
		t.Fatalf("%s sem combinado e fora da lista: %+v", nome, depois.SemCombinado)
	}
}

// Um vínculo de aluno num estúdio QUE NÃO É o do personal sob teste. O teste antigo de
// isolamento passava um UUID inexistente, o que prova só que UUID inventado não casa —
// nunca que um vínculo REAL de outro dono é recusado. Este cria o vínculo de verdade.
func bondDeOutroEstudio(t *testing.T, database *sql.DB, exceto string) string {
	t.Helper()
	ctx := context.Background()
	var dono, aluno, studio, bond string
	if err := database.QueryRowContext(ctx, `
		INSERT INTO people (phone, name) VALUES ('+5511999990001', 'Dona de outro estudio')
		ON CONFLICT (phone) DO UPDATE SET name = EXCLUDED.name
		RETURNING id::text`).Scan(&dono); err != nil {
		t.Fatal(err)
	}
	if dono == exceto {
		t.Fatal("o outro dono colidiu com o personal sob teste")
	}
	if err := database.QueryRowContext(ctx, `
		INSERT INTO studios (owner_person_id, name) VALUES ($1, 'Outro Estudio')
		ON CONFLICT (owner_person_id) DO UPDATE SET name = EXCLUDED.name
		RETURNING id::text`, dono).Scan(&studio); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(ctx, `
		INSERT INTO people (phone, name) VALUES ('+5511999990002', 'Aluna de outro estudio')
		ON CONFLICT (phone) DO UPDATE SET name = EXCLUDED.name
		RETURNING id::text`).Scan(&aluno); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(ctx, `
		INSERT INTO bonds (person_id, studio_id, role, status) VALUES ($1, $2, 'student', 'active')
		ON CONFLICT (person_id, studio_id) DO UPDATE SET status = 'active'
		RETURNING id::text`, aluno, studio).Scan(&bond); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO mensalidades (bond_id, amount_cents, due_day) VALUES ($1, 30000, 5)
		ON CONFLICT (bond_id) DO UPDATE SET amount_cents = 30000`, bond); err != nil {
		t.Fatal(err)
	}
	return bond
}

// O mesmo isolamento na escrita do PAGAMENTO: marcar recebido num vínculo de outro dono
// não pode gravar nem revelar que ele existe.
func TestPagarMensalidadeDeOutroEstudioRecusa(t *testing.T) {
	database := openSeeded(t)
	svc := New(database, nil)
	ctx := context.Background()
	fred := personIDByPhone(t, database, seed.PhoneFred)
	outro := bondDeOutroEstudio(t, database, fred)

	if err := svc.PagarMensalidade(ctx, fred, outro); err != ErrNotFound {
		t.Fatalf("pagou vinculo de outro estudio: %v", err)
	}
	var n int
	if err := database.QueryRow(
		`SELECT count(*) FROM mensalidade_pagamentos WHERE bond_id = $1`, outro,
	).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("gravou %d pagamento(s) num vinculo de outro dono", n)
	}
}

// O ciclo completo do dinheiro marcado à mão: marcar congela o valor, os três pedaços do
// mês andam juntos, e desfazer devolve tudo ao estado anterior. Antes, marcar era uma
// escrita sem volta e sem registro do valor.
func TestRecebiEDesfazer(t *testing.T) {
	database := openSeeded(t)
	svc := New(database, nil)
	ctx := context.Background()
	fred := personIDByPhone(t, database, seed.PhoneFred)

	antes, err := svc.Operacao(ctx, fred)
	if err != nil {
		t.Fatal(err)
	}
	// Os três pedaços somam o combinado: nada entra em dois lugares nem cai fora.
	soma := antes.RecebidoCents + antes.AVencerCents + antes.VencidoCents
	if soma != antes.ReceitaCents {
		t.Fatalf("recebido %d + a vencer %d + vencido %d = %d, mas o combinado e %d",
			antes.RecebidoCents, antes.AVencerCents, antes.VencidoCents, soma, antes.ReceitaCents)
	}
	if len(antes.EmAberto) == 0 {
		t.Fatal("preciso de alguem em aberto")
	}
	alvo := antes.EmAberto[0]

	if err := svc.PagarMensalidade(ctx, fred, alvo.BondID); err != nil {
		t.Fatal(err)
	}
	// O valor CONGELOU no fato: mudar o combinado depois não pode reescrever o passado.
	var gravado int
	var meio string
	if err := database.QueryRow(
		`SELECT amount_cents, meio FROM mensalidade_pagamentos WHERE bond_id = $1 AND month = $2::date`,
		alvo.BondID, antes.Month,
	).Scan(&gravado, &meio); err != nil {
		t.Fatal(err)
	}
	if gravado != alvo.AmountCents {
		t.Fatalf("gravou %d centavos, o combinado era %d", gravado, alvo.AmountCents)
	}
	if meio != "mao" {
		t.Fatalf("meio = %q", meio)
	}

	depois, err := svc.Operacao(ctx, fred)
	if err != nil {
		t.Fatal(err)
	}
	if depois.RecebidoCents != antes.RecebidoCents+alvo.AmountCents {
		t.Fatalf("recebido %d, esperava %d", depois.RecebidoCents, antes.RecebidoCents+alvo.AmountCents)
	}
	if len(depois.EmAberto) != len(antes.EmAberto)-1 {
		t.Fatalf("em aberto %d, esperava %d", len(depois.EmAberto), len(antes.EmAberto)-1)
	}
	if depois.UltimaMarcacao == nil {
		t.Fatal("ultima_marcacao nil depois de marcar")
	}
	// Depois de subir o combinado, o que ENTROU continua sendo o que entrou.
	if err := svc.DefinirMensalidade(ctx, fred, alvo.BondID, alvo.AmountCents+9900, 5); err != nil {
		t.Fatal(err)
	}
	comPrecoNovo, err := svc.Operacao(ctx, fred)
	if err != nil {
		t.Fatal(err)
	}
	if comPrecoNovo.RecebidoCents != depois.RecebidoCents {
		t.Fatalf("subir o preco reescreveu o recebido: %d -> %d",
			depois.RecebidoCents, comPrecoNovo.RecebidoCents)
	}

	// DESFAZER: volta a ficar em aberto, e repetir não é erro.
	if err := svc.DesfazerPagamento(ctx, fred, alvo.BondID); err != nil {
		t.Fatal(err)
	}
	if err := svc.DesfazerPagamento(ctx, fred, alvo.BondID); err != nil {
		t.Fatalf("desfazer duas vezes deu erro: %v", err)
	}
	final, err := svc.Operacao(ctx, fred)
	if err != nil {
		t.Fatal(err)
	}
	if final.RecebidoCents != antes.RecebidoCents {
		t.Fatalf("depois de desfazer, recebido %d != %d", final.RecebidoCents, antes.RecebidoCents)
	}
	voltou := false
	for _, it := range final.EmAberto {
		if it.BondID == alvo.BondID {
			voltou = true
		}
	}
	if !voltou {
		t.Fatal("desfez e a pessoa nao voltou para em aberto")
	}

	// Desfazer vínculo de outro estúdio não vaza nem apaga nada.
	if err := svc.DesfazerPagamento(ctx, fred, bondDeOutroEstudio(t, database, fred)); err != ErrNotFound {
		t.Fatalf("desfez de outro estudio: %v", err)
	}
}

// O pagamento de PROVEDOR nunca some por toque errado: o desfazer é do dedo, não do
// repasse. A trava vive no SQL (`AND pg.meio = 'mao'`) e não numa checagem em Go que a
// próxima rota esqueceria.
func TestDesfazerNaoApagaPagamentoDeProvedor(t *testing.T) {
	database := openSeeded(t)
	svc := New(database, nil)
	ctx := context.Background()
	fred := personIDByPhone(t, database, seed.PhoneFred)

	op, err := svc.Operacao(ctx, fred)
	if err != nil {
		t.Fatal(err)
	}
	alvo := op.EmAberto[0]
	if _, err := database.Exec(`
		INSERT INTO mensalidade_pagamentos (bond_id, month, amount_cents, meio)
		VALUES ($1, $2::date, $3, 'pix')`,
		alvo.BondID, op.Month, alvo.AmountCents,
	); err != nil {
		t.Fatal(err)
	}
	if err := svc.DesfazerPagamento(ctx, fred, alvo.BondID); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := database.QueryRow(
		`SELECT count(*) FROM mensalidade_pagamentos WHERE bond_id = $1 AND month = $2::date`,
		alvo.BondID, op.Month,
	).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("o desfazer apagou o pagamento do provedor: sobraram %d linhas", n)
	}
}

// O Pix viaja PRONTO na linha, com o valor daquela pessoa. Sem chave configurada, a linha
// simplesmente não tem código — e a tela cai para [Recebi], sem erro e sem tela quebrada.
func TestCopiaEColaNaLinhaEmAberto(t *testing.T) {
	database := openSeeded(t)
	svc := New(database, nil)
	ctx := context.Background()
	fred := personIDByPhone(t, database, seed.PhoneFred)

	// O estado inicial é ESTABELECIDO, não presumido: `chave_pix` é coluna nova e o seed
	// não a toca, então uma rodada anterior deste mesmo teste deixaria a chave gravada e o
	// caso "ainda não configurou" nunca seria exercido.
	if _, err := database.Exec(`
		UPDATE studios SET chave_pix = NULL, nome_recebedor = NULL, cidade_recebedor = NULL
		WHERE owner_person_id = $1`, fred); err != nil {
		t.Fatal(err)
	}
	semChave, err := svc.Operacao(ctx, fred)
	if err != nil {
		t.Fatal(err)
	}
	if semChave.Pix.Configurado {
		t.Fatal("limpei a chave e o payload diz que esta configurada")
	}
	for _, it := range semChave.EmAberto {
		if it.CopiaECola != "" {
			t.Fatalf("codigo sem chave configurada: %s", it.CopiaECola)
		}
	}

	chave, nome, cidade := "fred@studio.com.br", "Fred Personal", "Sao Paulo"
	if err := svc.PatchTime(ctx, fred, TimePatch{
		ChavePix: &chave, NomeRecebedor: &nome, CidadeRecebedor: &cidade,
	}); err != nil {
		t.Fatal(err)
	}
	comChave, err := svc.Operacao(ctx, fred)
	if err != nil {
		t.Fatal(err)
	}
	if !comChave.Pix.Configurado {
		t.Fatal("chave gravada e o payload diz que nao")
	}
	if len(comChave.EmAberto) == 0 {
		t.Fatal("preciso de uma linha em aberto")
	}
	for _, it := range comChave.EmAberto {
		if it.CopiaECola == "" {
			t.Fatalf("linha de %s sem codigo", it.Name)
		}
		// O VALOR DAQUELA PESSOA dentro do código é o ponto inteiro: é ele que impede a
		// aluna pagar R$ 300 quando o combinado é R$ 350.
		valor := fmt.Sprintf("54%02d%d.%02d",
			len(fmt.Sprintf("%d.%02d", it.AmountCents/100, it.AmountCents%100)),
			it.AmountCents/100, it.AmountCents%100)
		if !strings.Contains(it.CopiaECola, valor) {
			t.Fatalf("codigo de %s (R$ %d) nao traz o valor %q:\n%s",
				it.Name, it.AmountCents/100, valor, it.CopiaECola)
		}
	}
}
