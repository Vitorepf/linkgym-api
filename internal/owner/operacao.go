package owner

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"

	"github.com/Vitorepf/linkgym-api/internal/relogio"
)

// A OPERAÇÃO do personal: as leituras da Mensalidade sobre a turma (CONTEXT.md), a lista
// de quem está em aberto — com a ÚNICA ação que o domínio permite, marcar pago — e a
// lista de quem está perto de sumir. O app nunca cobra o aluno: tudo aqui é para os
// olhos e o dedo do personal.
type Operacao struct {
	StudentCount   int    `json:"student_count"`
	ComMensalidade int    `json:"com_mensalidade"`
	ReceitaCents   int    `json:"receita_cents"`
	TicketCents    int    `json:"ticket_cents"`
	Month          string `json:"month"`
	// O MÊS EM TRÊS PEDAÇOS, e não um "receita" que mistura os três. `receita_cents` é o
	// COMBINADO (preço de tabela × turma de hoje) e não muda um centavo quando ninguém
	// paga — chamar isso de receita é o app afirmando que entrou dinheiro que não entrou.
	// Estes três somam o combinado e dizem onde ele está.
	RecebidoCents int `json:"recebido_cents"`
	AVencerCents  int `json:"a_vencer_cents"`
	VencidoCents  int `json:"vencido_cents"`
	// Quando o personal marcou alguma coisa pela última vez. É o que permite a tela se
	// recusar a mentir: sem isto, "ninguém pagou" e "ele parou de marcar" são o mesmo
	// silêncio, e o app acusa a turma inteira de calote por esquecimento dele.
	UltimaMarcacao *string        `json:"ultima_marcacao"`
	EmAberto       []EmAbertoItem `json:"em_aberto"`
	Risco          []RiscoItem    `json:"risco"`
	// QUEM não tem combinado. O rodapé da tela já escrevia o NÚMERO ("8 sem valor
	// combinado ainda") e mandava o personal digitar "na pessoa" — uma porta que não
	// existia em tela nenhuma. Número sem os nomes que o compõem não abre porta: é o
	// mesmo defeito que a doutrina da casa aponta na tela "Retenção" da referência.
	SemCombinado []SemCombinadoItem `json:"sem_combinado"`
	// A chave Pix do personal, só para ele saber se já configurou e para a tela oferecer a
	// porta quando não. NÃO viaja no payload do aluno: `Time` é compartilhado com o convite
	// e com /v1/me do aluno, e chave de recebimento não é assunto de quem paga.
	Pix PixDoTime `json:"pix"`
	// O que ele vendeu fora da mensalidade e ainda não recebeu: avaliação, whey, marmita.
	AEntregar []Extra `json:"a_entregar"`
}

type PixDoTime struct {
	Configurado bool   `json:"configurado"`
	Chave       string `json:"chave"`
	Nome        string `json:"nome"`
	Cidade      string `json:"cidade"`
}

type SemCombinadoItem struct {
	BondID   string `json:"bond_id"`
	PersonID string `json:"person_id"`
	Name     string `json:"name"`
}

type EmAbertoItem struct {
	BondID      string `json:"bond_id"`
	PersonID    string `json:"person_id"`
	Name        string `json:"name"`
	AmountCents int    `json:"amount_cents"`
	DueDay      int    `json:"due_day"`
	// dias desde o vencimento neste mês; negativo = ainda vence, 0 = vence hoje.
	VencidoHa int `json:"vencido_ha"`
	// O copia-e-cola do Pix COM O VALOR desta linha. Vazio quando o personal ainda não
	// configurou a chave. Viaja no mesmo payload de propósito: um segundo pedido por linha
	// seria N idas à rede para um dado que já está aqui.
	CopiaECola string `json:"copia_e_cola"`
}

// RiscoItem mora em risco.go: o cálculo saiu daqui para ter DOIS consumidores (esta tela
// e a fila de atenção) sem duas regras que possam divergir.

func (s *Service) Operacao(ctx context.Context, ownerID string) (*Operacao, error) {
	studioID, err := s.ownerTime(ctx, ownerID)
	if err != nil {
		return nil, err
	}
	now := s.now()
	// A competência é civil e brasileira. Montada com time.UTC sobre a hora da máquina, no
	// dia 31 às 21h de Brasília ela já era o mês seguinte: a turma inteira voltava a dever
	// e o mês certo ficava em aberto sem caminho de quitação. Ver internal/relogio.
	month := relogio.Competencia(now)

	out := Operacao{
		Month:        month,
		EmAberto:     []EmAbertoItem{},
		Risco:        []RiscoItem{},
		SemCombinado: []SemCombinadoItem{},
	}

	if err := s.db.QueryRowContext(ctx, `
		SELECT count(*),
		       count(m.id),
		       COALESCE(SUM(m.amount_cents), 0)
		FROM bonds b
		LEFT JOIN mensalidades m ON m.bond_id = b.id
		WHERE b.studio_id = $1 AND b.role = 'student' AND b.status = 'active'`,
		studioID,
	).Scan(&out.StudentCount, &out.ComMensalidade, &out.ReceitaCents); err != nil {
		return nil, fmt.Errorf("operacao leituras: %w", err)
	}
	if out.ComMensalidade > 0 {
		out.TicketCents = out.ReceitaCents / out.ComMensalidade
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT b.id::text, p.id::text, p.name, m.amount_cents, m.due_day
		FROM bonds b
		JOIN people p ON p.id = b.person_id
		JOIN mensalidades m ON m.bond_id = b.id
		WHERE b.studio_id = $1 AND b.role = 'student' AND b.status = 'active'
		  AND NOT EXISTS (
			SELECT 1 FROM mensalidade_pagamentos pg
			WHERE pg.bond_id = b.id AND pg.month = $2::date
		  )
		ORDER BY m.due_day, p.name`,
		studioID, month,
	)
	if err != nil {
		return nil, fmt.Errorf("operacao em aberto: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var it EmAbertoItem
		if err := rows.Scan(&it.BondID, &it.PersonID, &it.Name, &it.AmountCents, &it.DueDay); err != nil {
			return nil, fmt.Errorf("operacao em aberto scan: %w", err)
		}
		it.VencidoHa = now.In(relogio.Fuso).Day() - it.DueDay
		// VENCIDO e A VENCER são coisas diferentes e estavam somadas no mesmo número. No
		// dia 4 do mês, com vencimentos em 5/10/15/20, NINGUÉM venceu — e a tela dizia
		// "EM ABERTO 28 · somam R$ 10.500", colado num "RECEITA DO MÊS R$ 10.500". Dois
		// números idênticos com sentidos opostos, e inadimplência real zero.
		if it.VencidoHa >= 0 {
			out.VencidoCents += it.AmountCents
		} else {
			out.AVencerCents += it.AmountCents
		}
		out.EmAberto = append(out.EmAberto, it)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("operacao em aberto rows: %w", err)
	}

	// O recebedor: UMA leitura, e o mesmo código vale para todas as linhas.
	quem := s.recebedorDe(ctx, studioID)
	out.Pix = PixDoTime{
		Configurado: quem.configurado(),
		Chave:       quem.chave,
		Nome:        quem.nome,
		Cidade:      quem.cidade,
	}
	for i := range out.EmAberto {
		out.EmAberto[i].CopiaECola = quem.codigo(out.EmAberto[i].AmountCents)
	}

	// O que ele vende fora da mensalidade e ainda não recebeu.
	avulsas, err := s.ExtrasAbertos(ctx, studioID)
	if err != nil {
		return nil, err
	}
	out.AEntregar = avulsas

	// O QUE ENTROU, do valor CONGELADO no fato — não do combinado de hoje. Subir o preço
	// de R$300 para R$350 em maio reescrevia o que entrou em janeiro.
	var ultima sql.NullString
	if err := s.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(pg.amount_cents), 0), MAX(pg.paid_at)::date::text
		FROM mensalidade_pagamentos pg
		JOIN bonds b ON b.id = pg.bond_id
		WHERE b.studio_id = $1 AND b.role = 'student' AND pg.month = $2::date`,
		studioID, month,
	).Scan(&out.RecebidoCents, &ultima); err != nil {
		return nil, fmt.Errorf("operacao recebido: %w", err)
	}
	if ultima.Valid {
		out.UltimaMarcacao = &ultima.String
	}

	// Os nomes por trás de "N sem valor combinado". Mesma cláusula das outras: só aluno
	// ativo deste estúdio, e a ausência é ausência de linha em `mensalidades`.
	semRows, err := s.db.QueryContext(ctx, `
		SELECT b.id::text, p.id::text, p.name
		FROM bonds b
		JOIN people p ON p.id = b.person_id
		WHERE b.studio_id = $1 AND b.role = 'student' AND b.status = 'active'
		  AND NOT EXISTS (SELECT 1 FROM mensalidades m WHERE m.bond_id = b.id)
		ORDER BY p.name`,
		studioID,
	)
	if err != nil {
		return nil, fmt.Errorf("operacao sem combinado: %w", err)
	}
	defer semRows.Close()
	for semRows.Next() {
		var it SemCombinadoItem
		if err := semRows.Scan(&it.BondID, &it.PersonID, &it.Name); err != nil {
			return nil, fmt.Errorf("operacao sem combinado scan: %w", err)
		}
		out.SemCombinado = append(out.SemCombinado, it)
	}
	if err := semRows.Err(); err != nil {
		return nil, fmt.Errorf("operacao sem combinado rows: %w", err)
	}

	// QUEM VAI SUMIR: uma chamada, e a regra mora em risco.go. Antes o cálculo estava
	// inline aqui e a fila de atenção do Painel tinha outra fonte — duas telas
	// respondendo à mesma pergunta com regras diferentes, uma delas alimentada por uma
	// tabela que nenhum processo de produção preenche.
	risco, err := s.Risco(ctx, studioID)
	if err != nil {
		return nil, err
	}
	out.Risco = risco

	return &out, nil
}

// Marca a mensalidade do Vínculo como PAGA no mês corrente. Fato, não estado: repetir o
// toque não duplica nem falha — ON CONFLICT DO NOTHING é o "já estava pago".
func (s *Service) PagarMensalidade(ctx context.Context, ownerID, bondID string) error {
	studioID, err := s.ownerTime(ctx, ownerID)
	if err != nil {
		return err
	}
	// A mesma competência civil da leitura: escrever num mês e ler de outro é como o
	// pagamento das 21h do dia 31 sumia.
	month := relogio.Competencia(s.now())

	// O VALOR VIAJA JUNTO, tirado de `mensalidades` no momento do fato. Sem isto o histórico
	// era recalculado do combinado de hoje, e corrigir o preço reescrevia o passado.
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO mensalidade_pagamentos (bond_id, month, amount_cents, meio)
		SELECT b.id, $2::date, m.amount_cents, 'mao'
		FROM bonds b
		JOIN mensalidades m ON m.bond_id = b.id
		WHERE b.id = $1 AND b.studio_id = $3 AND b.role = 'student' AND b.status = 'active'
		ON CONFLICT (bond_id, month) DO NOTHING`,
		bondID, month, studioID,
	)
	if err != nil {
		return fmt.Errorf("pagar mensalidade: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// Zero linhas tem TRÊS causas, e a conferência antiga só conhecia duas: o vínculo
		// não é deste time, já estava pago, OU o vínculo não está ativo. A pergunta antiga
		// perdia `role` e `status`, então um vínculo pausado casava aqui e a função
		// devolvia sucesso sem ter gravado nada — o personal via a linha sumir e acreditava
		// ter registrado um pagamento que não existe. Pergunte pelo FATO, não pelo vínculo.
		var pago bool
		if err := s.db.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM mensalidade_pagamentos pg
				JOIN bonds b ON b.id = pg.bond_id
				WHERE pg.bond_id = $1 AND pg.month = $2::date AND b.studio_id = $3
			)`, bondID, month, studioID,
		).Scan(&pago); err != nil {
			return fmt.Errorf("pagar mensalidade conferencia: %w", err)
		}
		if !pago {
			return ErrNotFound
		}
	}
	return nil
}

// DESFAZER o "Recebi". Marcar é a única escrita de dinheiro do produto e era write-only:
// um Pix que estorna, um comprovante de outro mês, um "vou receber hoje à noite" que não
// vem — todos terminavam com o mês marcado pago para sempre, e não havia nem onde ver o
// que foi marcado.
//
// Travado em `meio = 'mao'`: quando existir provedor, o fato dele não some por toque
// errado. Um dedo errado não é um fato; um repasse confirmado é.
func (s *Service) DesfazerPagamento(ctx context.Context, ownerID, bondID string) error {
	studioID, err := s.ownerTime(ctx, ownerID)
	if err != nil {
		return err
	}
	month := relogio.Competencia(s.now())

	res, err := s.db.ExecContext(ctx, `
		DELETE FROM mensalidade_pagamentos pg
		USING bonds b
		WHERE pg.bond_id = b.id
		  AND pg.bond_id = $1 AND pg.month = $2::date
		  AND b.studio_id = $3 AND pg.meio = 'mao'`,
		bondID, month, studioID,
	)
	if err != nil {
		return fmt.Errorf("desfazer pagamento: %w", err)
	}
	// Idempotente para frente: desfazer duas vezes não é erro, é a mesma verdade. Só
	// reclama quando o vínculo não é deste estúdio — e sem revelar que ele existe.
	if n, _ := res.RowsAffected(); n == 0 {
		var existe bool
		if err := s.db.QueryRowContext(ctx, `
			SELECT EXISTS (SELECT 1 FROM bonds b WHERE b.id = $1 AND b.studio_id = $2)`,
			bondID, studioID,
		).Scan(&existe); err != nil {
			return fmt.Errorf("desfazer pagamento conferencia: %w", err)
		}
		if !existe {
			return ErrNotFound
		}
	}
	return nil
}

// O combinado, digitado uma vez (e corrigível): valor e dia do vencimento do Vínculo.
func (s *Service) DefinirMensalidade(ctx context.Context, ownerID, bondID string, amountCents, dueDay int) error {
	if amountCents < 0 || dueDay < 1 || dueDay > 28 {
		return ErrInvalid
	}
	studioID, err := s.ownerTime(ctx, ownerID)
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO mensalidades (bond_id, amount_cents, due_day)
		SELECT b.id, $2, $3 FROM bonds b
		WHERE b.id = $1 AND b.studio_id = $4 AND b.role = 'student' AND b.status = 'active'
		ON CONFLICT (bond_id) DO UPDATE
		SET amount_cents = EXCLUDED.amount_cents, due_day = EXCLUDED.due_day, updated_at = now()`,
		bondID, amountCents, dueDay, studioID,
	)
	if err != nil {
		return fmt.Errorf("definir mensalidade: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

var hexColor = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

// O que o personal escreve sobre o próprio Time. Ponteiro nil = não veio no corpo e não se
// toca na coluna; string vazia, onde faz sentido, LIMPA.
type TimePatch struct {
	Name            *string
	Accent          *string
	LogoKey         *string
	ChavePix        *string
	NomeRecebedor   *string
	CidadeRecebedor *string
}

// Vazio: nada veio no corpo. O handler pergunta para não gravar por engano e para devolver
// 400 em vez de 200 silencioso.
func (p TimePatch) Vazio() bool {
	return p.Name == nil && p.Accent == nil && p.LogoKey == nil &&
		p.ChavePix == nil && p.NomeRecebedor == nil && p.CidadeRecebedor == nil
}

// White-label: nome, acento e LOGO são DO TIME, e é o personal quem os escreve. O schema já
// garante o hex; validar aqui devolve 400 em vez de 500. Logo com string vazia LIMPA
// (voltar às iniciais é escolha válida); a chave do logo tem que ser do prefixo do próprio
// time.
//
// A CHAVE PIX entra aqui pelo mesmo caminho, e pelo mesmo motivo: é a marca dele operando.
// Ela nunca vira conta da plataforma — o BR Code é montado a partir dela e o dinheiro do
// aluno cai direto na conta do personal.
func (s *Service) PatchTime(ctx context.Context, ownerID string, p TimePatch) error {
	if p.Vazio() {
		return ErrInvalid
	}
	if p.Name != nil && strings.TrimSpace(*p.Name) == "" {
		return ErrInvalid
	}
	if p.Accent != nil && !hexColor.MatchString(*p.Accent) {
		return ErrInvalid
	}
	studioID, err := s.ownerTime(ctx, ownerID)
	if err != nil {
		return err
	}
	if p.LogoKey != nil && *p.LogoKey != "" && !strings.HasPrefix(*p.LogoKey, "logo/"+studioID+"/") {
		return ErrInvalid
	}

	// Coluna e valor, em pares. Eram seis blocos UPDATE idênticos a menos do nome da
	// coluna, e cada campo novo do white-label acrescentava mais um. Os nomes de coluna são
	// literais deste arquivo — nunca entrada de usuário —, então a interpolação é segura e o
	// valor continua indo por parâmetro.
	type par struct {
		coluna string
		valor  string
		limpa  bool // string vazia grava NULL
	}
	var campos []par
	if p.Name != nil {
		campos = append(campos, par{"name", strings.TrimSpace(*p.Name), false})
	}
	if p.Accent != nil {
		campos = append(campos, par{"accent_color", *p.Accent, false})
	}
	if p.LogoKey != nil {
		campos = append(campos, par{"logo_object_key", *p.LogoKey, true})
	}
	if p.ChavePix != nil {
		campos = append(campos, par{"chave_pix", strings.TrimSpace(*p.ChavePix), true})
	}
	if p.NomeRecebedor != nil {
		campos = append(campos, par{"nome_recebedor", strings.TrimSpace(*p.NomeRecebedor), true})
	}
	if p.CidadeRecebedor != nil {
		campos = append(campos, par{"cidade_recebedor", strings.TrimSpace(*p.CidadeRecebedor), true})
	}
	for _, c := range campos {
		valor := "$2"
		if c.limpa {
			valor = "NULLIF($2, '')"
		}
		if _, err := s.db.ExecContext(ctx,
			"UPDATE studios SET "+c.coluna+" = "+valor+", updated_at = now() WHERE id = $1",
			studioID, c.valor,
		); err != nil {
			return fmt.Errorf("patch time %s: %w", c.coluna, err)
		}
	}
	return nil
}
