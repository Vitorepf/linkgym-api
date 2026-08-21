package owner

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Vitorepf/linkgym-api/internal/relogio"
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
		// E é a DÍVIDA INTEIRA, não um mês: a linha ao lado já imprime "5 meses · R$ 1.750"
		// e o código carregava R$ 350 — o personal colava no WhatsApp um pedido de um quinto
		// do que tem a receber.
		valor := fmt.Sprintf("54%02d%d.%02d",
			len(fmt.Sprintf("%d.%02d", it.DevidoCents/100, it.DevidoCents%100)),
			it.DevidoCents/100, it.DevidoCents%100)
		if !strings.Contains(it.CopiaECola, valor) {
			t.Fatalf("codigo de %s (R$ %d) nao traz o valor %q:\n%s",
				it.Name, it.DevidoCents/100, valor, it.CopiaECola)
		}
	}
}

// O ATRASO ATRAVESSA A VIRADA DO MÊS, e não nasce do nada no primeiro dia.
//
// A conta antiga era `hoje.Day() - due_day` e tinha dois defeitos de raiz que este teste
// prende: (a) quem devia desde julho lia "venceu há 20 dias" em 30/08 e "venceu há 1 dia"
// em 01/09 — a dívida DOBRA e o sinal enfraquece; (b) com due_day 27 ou 28, alcançar 5 dias
// de atraso exigiria o dia 32 do mês, então o vencimento mais natural do país nunca
// disparava o sinal forte. E o terceiro: quem entrou dia 20 nascia devendo desde o dia 1.
func TestAtrasoContaDeDataENaoDeDiaDoMes(t *testing.T) {
	database := openSeeded(t)
	ctx := context.Background()
	fred := personIDByPhone(t, database, seed.PhoneFred)

	base, err := New(database, nil).Operacao(ctx, fred)
	if err != nil {
		t.Fatal(err)
	}
	alvo := base.EmAberto[0]

	// due_day 28 — o fim do mês, que a conta antiga nunca alcançava.
	if _, err := database.Exec(
		`UPDATE mensalidades SET due_day = 28 WHERE bond_id = $1`, alvo.BondID); err != nil {
		t.Fatal(err)
	}
	// Vínculo antigo, sem nenhum pagamento: deve desde que entrou.
	if _, err := database.Exec(
		`UPDATE bonds SET created_at = now() - interval '400 days' WHERE id = $1`, alvo.BondID); err != nil {
		t.Fatal(err)
	}
	// E O COMBINADO TEM A IDADE DA DÍVIDA. Este UPDATE faltava, e sem ele o teste exigia
	// 300 dias de atraso sobre um acordo com `created_at = now()` — cobrar por meses
	// anteriores ao acordo, que é exatamente o que TestDividaDeVariosMeses proíbe três
	// testes abaixo. Enquanto a contagem e o atraso saíam de janelas DIFERENTES a
	// contradição passava: uma partia do combinado, a outra do vínculo.
	if _, err := database.Exec(
		`UPDATE mensalidades SET created_at = now() - interval '400 days' WHERE bond_id = $1`,
		alvo.BondID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(
		`DELETE FROM mensalidade_pagamentos WHERE bond_id = $1`, alvo.BondID); err != nil {
		t.Fatal(err)
	}

	// DIA 1º: o pior dia para a conta antiga, que zerava ali.
	primeiro := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	no1, err := New(database, func() time.Time { return primeiro }).Operacao(ctx, fred)
	if err != nil {
		t.Fatal(err)
	}
	var linha *EmAbertoItem
	for i := range no1.EmAberto {
		if no1.EmAberto[i].BondID == alvo.BondID {
			linha = &no1.EmAberto[i]
		}
	}
	if linha == nil {
		t.Fatal("quem nunca pagou saiu da lista")
	}
	// A conta antiga daria 1 - 28 = -27 ("vence dia 28"). A dívida é de mais de um ano.
	if linha.VencidoHa < 300 {
		t.Fatalf("no dia 1o o atraso zerou: vencido_ha = %d", linha.VencidoHa)
	}

	// QUEM ENTROU ONTEM não deve nada, mesmo com o vencimento já tendo passado no mês.
	// A data do vínculo é ABSOLUTA e ancorada no relógio injetado: criar "há 1 dia" em
	// tempo real e consultar com um relógio no futuro é um vínculo velho, não um novo — e
	// aí o atraso é verdadeiro.
	hoje := time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)
	novo := bondNovoCom(t, database, fred, 5, "2026-09-14")
	depois, err := New(database, func() time.Time { return hoje }).Operacao(ctx, fred)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range depois.EmAberto {
		if it.BondID == novo && it.VencidoHa > 1 {
			t.Fatalf("aluno que entrou ontem nasceu devendo ha %d dias", it.VencidoHa)
		}
	}
	if _, err := database.Exec(`DELETE FROM bonds WHERE id = $1`, novo); err != nil {
		t.Fatal(err)
	}
}

// Um vínculo novo neste estúdio, com mensalidade, criado `diasAtras` dias atrás.
func bondNovoCom(t *testing.T, database *sql.DB, ownerID string, dueDay int, entrouEm string) string {
	t.Helper()
	var studio string
	if err := database.QueryRow(
		`SELECT s.id::text FROM studios s WHERE s.owner_person_id = $1`, ownerID).Scan(&studio); err != nil {
		t.Fatal(err)
	}
	var person string
	if err := database.QueryRow(`
		INSERT INTO people (phone, name) VALUES ('+5511900888001', 'Recem chegada')
		ON CONFLICT (phone) DO UPDATE SET name = EXCLUDED.name
		RETURNING id::text`).Scan(&person); err != nil {
		t.Fatal(err)
	}
	var bond string
	if err := database.QueryRow(`
		INSERT INTO bonds (person_id, studio_id, role, status, created_at)
		VALUES ($1, $2, 'student', 'active', $3::date)
		ON CONFLICT (person_id, studio_id) DO UPDATE
		SET status = 'active', created_at = EXCLUDED.created_at
		RETURNING id::text`, person, studio, entrouEm).Scan(&bond); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		INSERT INTO mensalidades (bond_id, amount_cents, due_day) VALUES ($1, 30000, $2)
		ON CONFLICT (bond_id) DO UPDATE SET due_day = EXCLUDED.due_day`, bond, dueDay); err != nil {
		t.Fatal(err)
	}
	return bond
}

// A CHAVE PIX é conferida antes de gravar. Sem isto, o BR Code sai sintaticamente válido e
// semanticamente morto: o banco da aluna abre e diz "chave não encontrada" na frente dela, e
// o personal não tem como saber que o problema é um ponto a mais no CPF.
func TestChavePixNormalizaERecusa(t *testing.T) {
	bons := map[string]string{
		"123.456.789-01":                       "12345678901",
		"12345678901":                          "12345678901",
		"12.345.678/0001-95":                   "12345678000195",
		"(11) 98888-7777":                      "+5511988887777",
		"+55 11 98888-7777":                    "+5511988887777",
		"1188887777":                           "+551188887777",
		"Fred@Studio.com.BR":                   "fred@studio.com.br",
		"e1c4d2f0-3a5b-4c6d-8e9f-0a1b2c3d4e5f": "e1c4d2f0-3a5b-4c6d-8e9f-0a1b2c3d4e5f",
		"":                                     "",
	}
	for entrada, quer := range bons {
		got, ok := normalizarChavePix(entrada)
		if !ok || got != quer {
			t.Errorf("normalizarChavePix(%q) = (%q, %v), queria (%q, true)", entrada, got, ok, quer)
		}
	}
	ruins := []string{
		"fred", "1234", "123456789012345", "@studio.com", "fred@studio",
		"não é chave", "1234567890123",
	}
	for _, r := range ruins {
		if _, ok := normalizarChavePix(r); ok {
			t.Errorf("normalizarChavePix(%q) aceitou", r)
		}
	}

	// E a recusa chega como ErrInvalid, que o handler traduz em 400 — nunca 500.
	database := openSeeded(t)
	svc := New(database, nil)
	fred := personIDByPhone(t, database, seed.PhoneFred)
	ruim := "isso nao e chave"
	if err := svc.PatchTime(context.Background(), fred, TimePatch{ChavePix: &ruim}); err != ErrInvalid {
		t.Fatalf("chave invalida gravou: %v", err)
	}
	// Vazio LIMPA: voltar a não receber pelo app é escolha válida.
	vazio := ""
	if err := svc.PatchTime(context.Background(), fred, TimePatch{ChavePix: &vazio}); err != nil {
		t.Fatalf("limpar a chave deu erro: %v", err)
	}
}

// A OPERAÇÃO INTEIRA: os números que o personal não tem em lugar nenhum. Cada um deles
// muda o que ele faz no mês seguinte — não é painel de vaidade.
func TestVisaoDoMes(t *testing.T) {
	database := openSeeded(t)
	svc := New(database, nil)
	ctx := context.Background()
	fred := personIDByPhone(t, database, seed.PhoneFred)

	op, err := svc.Operacao(ctx, fred)
	if err != nil {
		t.Fatal(err)
	}
	// Permanência dos ATIVOS: o seed cria a turma agora, então é perto de zero — mas o
	// campo tem que existir e ser não-negativo, nunca NaN.
	if op.Visao.PermanenciaMeses < 0 {
		t.Fatalf("permanencia %v", op.Visao.PermanenciaMeses)
	}
	// CONCENTRAÇÃO: com todo mundo combinado, a maior fatia é maior que zero e menor que
	// 100%. Um estúdio de 28 alunos onde um deles é 100% da receita é um estúdio de um.
	if op.Visao.MaiorFatiaBps <= 0 || op.Visao.MaiorFatiaBps >= 10000 {
		t.Fatalf("maior fatia = %d bps", op.Visao.MaiorFatiaBps)
	}
	if op.Visao.MaiorNome == "" {
		t.Fatal("a maior fatia sem o nome de quem e: numero sem porta")
	}

	// SAIU alguém: o número tem que enxergar.
	alvo := op.EmAberto[0].BondID
	if err := svc.MudarEstadoDoVinculo(ctx, fred, alvo, VinculoEncerrado); err != nil {
		t.Fatal(err)
	}
	depois, err := svc.Operacao(ctx, fred)
	if err != nil {
		t.Fatal(err)
	}
	if depois.Visao.Sairam != op.Visao.Sairam+1 {
		t.Fatalf("sairam %d, esperava %d", depois.Visao.Sairam, op.Visao.Sairam+1)
	}
	// E o total de alunos anda junto — os dois números vêm da mesma verdade.
	if depois.StudentCount != op.StudentCount-1 {
		t.Fatalf("saiu 1 e o total foi de %d para %d", op.StudentCount, depois.StudentCount)
	}
	if err := svc.MudarEstadoDoVinculo(ctx, fred, alvo, VinculoAtivo); err != nil {
		t.Fatal(err)
	}
}

// A DÍVIDA DE VÁRIOS MESES aparece com o tamanho dela. A lista é escopada pelo mês, e por
// isso o valor da linha era sempre o de UM mês: quem devia desde junho lia "venceu há 3
// meses · R$ 350" quando o fato eram R$ 1.050. O atraso já atravessava a virada; o valor não.
func TestDividaDeVariosMesesTemOTamanhoDela(t *testing.T) {
	database := openSeeded(t)
	ctx := context.Background()
	fred := personIDByPhone(t, database, seed.PhoneFred)

	base, err := New(database, nil).Operacao(ctx, fred)
	if err != nil {
		t.Fatal(err)
	}
	alvo := base.EmAberto[0]

	// O CENÁRIO É RESTAURADO: este teste muda o vínculo e a mensalidade de alguém do seed, e
	// o seed é o cenário fixo dos testes vizinhos (o Jose tem um teste de fila só dele). Sem
	// isto, um teste de dívida quebra dois testes que não têm nada a ver com dívida.
	t.Cleanup(func() {
		_, _ = database.Exec(`
			UPDATE mensalidades SET created_at = now(), amount_cents = $2 WHERE bond_id = $1`,
			alvo.BondID, alvo.AmountCents)
		_, _ = database.Exec(`UPDATE bonds SET created_at = now() WHERE id = $1`, alvo.BondID)
	})

	// Combinado feito há 4 meses, nada pago desde então.
	if _, err := database.Exec(`
		UPDATE mensalidades SET created_at = now() - interval '4 months', amount_cents = 35000
		WHERE bond_id = $1`, alvo.BondID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		UPDATE bonds SET created_at = now() - interval '10 months' WHERE id = $1`, alvo.BondID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(
		`DELETE FROM mensalidade_pagamentos WHERE bond_id = $1`, alvo.BondID); err != nil {
		t.Fatal(err)
	}

	got, err := New(database, nil).Operacao(ctx, fred)
	if err != nil {
		t.Fatal(err)
	}
	var linha *EmAbertoItem
	for i := range got.EmAberto {
		if got.EmAberto[i].BondID == alvo.BondID {
			linha = &got.EmAberto[i]
		}
	}
	if linha == nil {
		t.Fatal("quem deve 5 competencias saiu da lista")
	}
	// 5 competências: o mês do combinado e os quatro seguintes até o corrente.
	if linha.MesesAbertos != 5 {
		t.Fatalf("meses_abertos = %d, esperava 5", linha.MesesAbertos)
	}
	if linha.DevidoCents != 5*35000 {
		t.Fatalf("devido = %d, esperava %d", linha.DevidoCents, 5*35000)
	}
	// A CONTAGEM COMEÇA NO COMBINADO, não no vínculo: o vínculo tem 10 meses e o acordo 4.
	// Cobrar por meses anteriores ao acordo seria inventar dívida.
	if linha.MesesAbertos > 6 {
		t.Fatalf("contou %d meses: esta cobrando de antes do combinado", linha.MesesAbertos)
	}

	// A BARRA DO MÊS continua sendo do MÊS: ela não incha com a dívida antiga, senão a tela
	// passa a ter dois números com o mesmo nome e sentidos diferentes — o defeito que a
	// reconstrução inteira existiu para matar.
	soma := got.RecebidoCents + got.AVencerCents + got.VencidoCents
	if soma != got.ReceitaCents {
		t.Fatalf("a barra do mes deixou de fechar: %d != %d", soma, got.ReceitaCents)
	}
}

// O TOQUE QUITA A COMPETÊNCIA MAIS ANTIGA, e a pessoa NÃO some da tela enquanto dever.
//
// Era o pior estado que o produto sabia criar e não sabia desfazer: [Recebi] gravava sempre
// no mês corrente e a lista era escopada pelo mesmo mês. Quem devia 5 competências recebia
// UM toque, agosto era marcado, ela sumia da lista, e R$ 1.400 em quatro competências
// ficavam abertos para sempre — sem nenhuma linha, botão ou tela que os alcançasse.
func TestQuitarComecaPelaCompetenciaMaisAntiga(t *testing.T) {
	database := openSeeded(t)
	svc := New(database, nil)
	ctx := context.Background()
	fred := personIDByPhone(t, database, seed.PhoneFred)

	base, err := svc.Operacao(ctx, fred)
	if err != nil {
		t.Fatal(err)
	}
	if len(base.EmAberto) == 0 {
		t.Fatal("preciso de alguem em aberto")
	}
	alvo := base.EmAberto[0]
	t.Cleanup(func() {
		_, _ = database.Exec(
			`UPDATE mensalidades SET created_at = now(), amount_cents = $2 WHERE bond_id = $1`,
			alvo.BondID, alvo.AmountCents)
		_, _ = database.Exec(`UPDATE bonds SET created_at = now() WHERE id = $1`, alvo.BondID)
	})

	// Combinado e vínculo de 4 meses atrás, nada pago: 5 competências abertas.
	for _, q := range []string{
		`UPDATE mensalidades SET created_at = now() - interval '4 months' WHERE bond_id = $1`,
		`UPDATE bonds SET created_at = now() - interval '4 months' WHERE id = $1`,
		`DELETE FROM mensalidade_pagamentos WHERE bond_id = $1`,
	} {
		if _, err := database.Exec(q, alvo.BondID); err != nil {
			t.Fatal(err)
		}
	}
	linha := func(o *Operacao) *EmAbertoItem {
		for i := range o.EmAberto {
			if o.EmAberto[i].BondID == alvo.BondID {
				return &o.EmAberto[i]
			}
		}
		return nil
	}

	devendo, err := svc.Operacao(ctx, fred)
	if err != nil {
		t.Fatal(err)
	}
	if l := linha(devendo); l == nil || l.MesesAbertos != 5 {
		t.Fatalf("cenario errado: %+v", l)
	}

	if err := svc.PagarMensalidade(ctx, fred, alvo.BondID); err != nil {
		t.Fatal(err)
	}
	// O FATO caiu na competência mais antiga, e não em agosto.
	var mes string
	if err := database.QueryRow(
		`SELECT min(month)::text FROM mensalidade_pagamentos WHERE bond_id = $1`, alvo.BondID,
	).Scan(&mes); err != nil {
		t.Fatal(err)
	}
	var esperado string
	if err := database.QueryRow(
		`SELECT date_trunc('month', now() - interval '4 months')::date::text`).Scan(&esperado); err != nil {
		t.Fatal(err)
	}
	if mes != esperado {
		t.Fatalf("quitou %s, esperava a mais antiga (%s)", mes, esperado)
	}

	// E ELA CONTINUA NA TELA, com uma competência a menos. Aqui a pessoa sumia.
	umPago, err := svc.Operacao(ctx, fred)
	if err != nil {
		t.Fatal(err)
	}
	l := linha(umPago)
	if l == nil {
		t.Fatal("quitou a mais antiga e a devedora sumiu da lista")
	}
	if l.MesesAbertos != 4 || l.DevidoCents != 4*l.AmountCents {
		t.Fatalf("meses %d / devido %d, esperava 4 e %d", l.MesesAbertos, l.DevidoCents, 4*l.AmountCents)
	}
	// A BARRA continua sendo do MÊS: o dinheiro de abril não entra no recebido de agosto,
	// e os três pedaços continuam fechando no combinado.
	if umPago.RecebidoCents != devendo.RecebidoCents {
		t.Fatalf("dinheiro de outra competencia entrou no recebido do mes: %d -> %d",
			devendo.RecebidoCents, umPago.RecebidoCents)
	}
	if s := umPago.RecebidoCents + umPago.AVencerCents + umPago.VencidoCents; s != umPago.ReceitaCents {
		t.Fatalf("a barra do mes deixou de fechar: %d != %d", s, umPago.ReceitaCents)
	}
	// ...mas ele MARCOU alguma coisa, e a tela não pode dizer "nada marcado ainda".
	if umPago.UltimaMarcacao == nil {
		t.Fatal("marcou a competencia antiga e ultima_marcacao ficou nil")
	}

	// E O DINHEIRO JÁ ESCONDIDO VOLTA A EXISTIR. Este é o estado que qualquer banco tocado
	// pelo código antigo já tem: a competência CORRENTE paga e as antigas abertas. Enquanto
	// a lista era escopada por "NOT EXISTS pagamento do mês corrente", essa pessoa não
	// existia em nenhuma tela do produto — e nenhum toque alcançava a dívida dela.
	if _, err := database.Exec(`
		INSERT INTO mensalidade_pagamentos (bond_id, month, amount_cents, meio)
		SELECT $1, date_trunc('month', now())::date, m.amount_cents, 'mao'
		FROM mensalidades m WHERE m.bond_id = $1
		ON CONFLICT (bond_id, month) DO NOTHING`, alvo.BondID); err != nil {
		t.Fatal(err)
	}
	fantasma, err := svc.Operacao(ctx, fred)
	if err != nil {
		t.Fatal(err)
	}
	lf := linha(fantasma)
	if lf == nil {
		t.Fatal("pagou o mes corrente e a divida antiga sumiu da tela inteira")
	}
	if lf.MesesAbertos != 3 || lf.DevidoCents != 3*lf.AmountCents {
		t.Fatalf("meses %d / devido %d, esperava 3", lf.MesesAbertos, lf.DevidoCents)
	}
	// E A BARRA CONTINUA FECHANDO: o mês corrente dela está quitado, então ela entra na
	// LISTA (a dívida existe) e não entra em vencido/a_vencer (o mês está pago).
	if s := fantasma.RecebidoCents + fantasma.AVencerCents + fantasma.VencidoCents; s != fantasma.ReceitaCents {
		t.Fatalf("a barra do mes deixou de fechar: %d != %d", s, fantasma.ReceitaCents)
	}
	if _, err := database.Exec(
		`DELETE FROM mensalidade_pagamentos WHERE bond_id = $1 AND month = date_trunc('month', now())::date`,
		alvo.BondID); err != nil {
		t.Fatal(err)
	}

	// DESFAZER é do último FATO: escopado pelo mês corrente, o DELETE procurava agosto,
	// não achava a linha de abril e devolvia sucesso silencioso.
	if err := svc.DesfazerPagamento(ctx, fred, alvo.BondID); err != nil {
		t.Fatal(err)
	}
	voltou, err := svc.Operacao(ctx, fred)
	if err != nil {
		t.Fatal(err)
	}
	if l := linha(voltou); l == nil || l.MesesAbertos != 5 {
		t.Fatalf("desfez e a divida nao voltou ao tamanho dela: %+v", l)
	}
}

// A ESCADA NÃO PODE DISCORDAR DA MÉDIA QUE ELA DESMENTE. `combinados` são as MESMAS linhas
// que produzem `receita_cents` e `ticket_cents`, sem o SUM — então a soma daqui é aquele
// número, e a contagem é `com_mensalidade`. Se divergir, o desenho vira uma segunda verdade
// sobre o mesmo dinheiro, que é como o personal para de acreditar nos dois.
func TestCombinadosSomamAReceita(t *testing.T) {
	database := openSeeded(t)
	svc := New(database, nil)
	fred := personIDByPhone(t, database, seed.PhoneFred)

	op, err := svc.Operacao(context.Background(), fred)
	if err != nil {
		t.Fatal(err)
	}
	if len(op.Combinados) != op.ComMensalidade {
		t.Fatalf("combinados %d, com_mensalidade %d", len(op.Combinados), op.ComMensalidade)
	}
	soma := 0
	for _, c := range op.Combinados {
		soma += c.AmountCents
		if c.Name == "" || c.BondID == "" || c.AmountCents <= 0 || c.DueDay < 1 {
			t.Fatalf("linha incompleta: %+v", c)
		}
	}
	if soma != op.ReceitaCents {
		t.Fatalf("soma dos combinados %d, receita_cents %d", soma, op.ReceitaCents)
	}
	// Em ordem de preço: a escada monta faixas contíguas em cima disso.
	for i := 1; i < len(op.Combinados); i++ {
		if op.Combinados[i-1].AmountCents > op.Combinados[i].AmountCents {
			t.Fatalf("fora de ordem em %d", i)
		}
	}
}

// A DATA QUE ELE PEDIU POR ESCRITO. "Quem nao pagou ainda E A DATA" — e a tela nao tinha uma
// unica data de calendario. `venceu_em` e a MESMA competencia de onde sai `vencido_ha`: um
// ponto so, ou o dia e o "ha N dias" divergem na mesma linha.
func TestEmAbertoCarregaADataDoVencimento(t *testing.T) {
	database := openSeeded(t)
	svc := New(database, nil)
	fred := personIDByPhone(t, database, seed.PhoneFred)

	op, err := svc.Operacao(context.Background(), fred)
	if err != nil {
		t.Fatal(err)
	}
	if len(op.EmAberto) == 0 {
		t.Skip("ninguem em aberto na semente")
	}
	for _, it := range op.EmAberto {
		if len(it.VenceuEm) != 10 || it.VenceuEm[4] != '-' || it.VenceuEm[7] != '-' {
			t.Fatalf("%s: venceu_em = %q, esperado ISO", it.Name, it.VenceuEm)
		}
		// O DIA da data e o due_day: a data e o vencimento, nao o inicio da competencia.
		dia, err := strconv.Atoi(it.VenceuEm[8:])
		if err != nil {
			t.Fatal(err)
		}
		if dia != it.DueDay {
			t.Fatalf("%s: venceu_em dia %d, due_day %d", it.Name, dia, it.DueDay)
		}
		// E ela BATE com o atraso: os dois saem da mesma competencia.
		venceu, err := time.Parse("2006-01-02", it.VenceuEm)
		if err != nil {
			t.Fatal(err)
		}
		hoje, err := time.Parse("2006-01-02", relogio.Dia(svc.now()))
		if err != nil {
			t.Fatal(err)
		}
		peloDia := int(hoje.Sub(venceu).Hours() / 24)
		// vencido_ha e limitado pela idade do vinculo, entao ele nunca PASSA do calculo
		// pela data — mas nao pode ser maior, que seria divida inventada.
		if it.VencidoHa > peloDia {
			t.Fatalf("%s: vencido_ha %d > %d dias desde %s", it.Name, it.VencidoHa, peloDia, it.VenceuEm)
		}
	}
}
