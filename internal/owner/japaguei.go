package owner

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/Vitorepf/linkgym-api/internal/relogio"
)

// O ALUNO GANHA O VERBO — e ele NÃO MARCA NADA.
//
// Toda a Operação é aritmética sobre um gesto digitado à mão: o personal tocar "Recebi", 28
// vezes por mês, todo mês, para sempre, sem nenhuma força que o obrigue. No terceiro mês ele
// vai parar. E o modo como o produto falha aí não é ficar em branco — é MENTIR NA PIOR
// DIREÇÃO, porque "em aberto = ausência de linha" torna o esquecimento dele indistinguível
// de calote. A tela passa a gritar que a turma inteira deve, a fila de risco enche de falso
// positivo, e a credibilidade da fila — o ativo inteiro do lado do personal — morre em uma
// semana.
//
// O aluno é a única outra pessoa do sistema com incentivo PRÓPRIO de corrigir isso: quem
// pagou não quer aparecer devendo. Então ele ganha uma frase, e só uma: "já paguei". Ela não
// registra pagamento, não notifica ninguém, não muda um centavo em lugar nenhum. Ela vira um
// NOME no topo da lista do personal, para o dedo dele confirmar. Um toque dele, zero robô.
//
// A LEI DESTA TELA, e ela vale para o código todo: a linha do lado do aluno NUNCA muda de
// tom. Nunca diz vencido, nunca diz atrasado, nunca fica vermelha, nunca vira push. Ela
// enuncia o combinado e oferece o verbo. O app não cobra o aluno — quem cobra é o personal,
// com a frase dele.
type MensalidadeDoAluno struct {
	// nil quando o personal ainda não digitou o combinado: a tela não desenha nada em vez
	// de escrever R$ 0, que seria o app afirmando que esta pessoa treina de graça.
	ValorCents *int   `json:"valor_cents"`
	DueDay     *int   `json:"due_day"`
	Month      string `json:"month"`
	// se ela já disse, neste mês. O botão vira a confirmação e some do caminho.
	JaDisse bool `json:"ja_disse"`
	// se o personal já registrou o recebimento deste mês. A linha inteira some: não há o
	// que dizer sobre um mês fechado.
	Recebido bool `json:"recebido"`
}

// O combinado desta pessoa, e nada mais. Nunca devolve estado de atraso — não existe campo
// para isso aqui de propósito, para que nenhuma tela futura consiga desenhar um.
func (s *Service) MensalidadeDoAluno(ctx context.Context, personID string) (*MensalidadeDoAluno, error) {
	month := relogio.Competencia(s.now())
	out := MensalidadeDoAluno{Month: month}

	var valor, dia sql.NullInt64
	err := s.db.QueryRowContext(ctx, `
		SELECT m.amount_cents, m.due_day,
		       EXISTS (SELECT 1 FROM ja_paguei j WHERE j.bond_id = b.id AND j.month = $2::date),
		       EXISTS (SELECT 1 FROM mensalidade_pagamentos pg
		                WHERE pg.bond_id = b.id AND pg.month = $2::date)
		FROM bonds b
		LEFT JOIN mensalidades m ON m.bond_id = b.id
		WHERE b.person_id = $1 AND b.role = 'student' AND b.status = 'active'
		LIMIT 1`,
		personID, month,
	).Scan(&valor, &dia, &out.JaDisse, &out.Recebido)
	if err == sql.ErrNoRows {
		return &out, nil
	}
	if err != nil {
		return nil, fmt.Errorf("mensalidade do aluno: %w", err)
	}
	if valid := valor.Valid && dia.Valid; valid {
		v, d := int(valor.Int64), int(dia.Int64)
		out.ValorCents, out.DueDay = &v, &d
	}
	return &out, nil
}

// "Já paguei": um fato dito, não um pagamento. Idempotente pela chave primária composta —
// tocar duas vezes é a mesma verdade, e o produto inteiro trata repetição assim.
func (s *Service) DizerQueJaPagou(ctx context.Context, personID string) error {
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO ja_paguei (bond_id, month)
		SELECT b.id, $2::date FROM bonds b
		WHERE b.person_id = $1 AND b.role = 'student' AND b.status = 'active'
		ON CONFLICT (bond_id, month) DO NOTHING`,
		personID, relogio.Competencia(s.now()),
	)
	if err != nil {
		return fmt.Errorf("ja paguei: %w", err)
	}
	// Zero linhas com vínculo ativo = já tinha dito. Sem vínculo, não há o que dizer.
	if n, _ := res.RowsAffected(); n == 0 {
		var existe bool
		if err := s.db.QueryRowContext(ctx, `
			SELECT EXISTS (SELECT 1 FROM bonds b
			                WHERE b.person_id = $1 AND b.role = 'student' AND b.status = 'active')`,
			personID,
		).Scan(&existe); err != nil {
			return fmt.Errorf("ja paguei conferencia: %w", err)
		}
		if !existe {
			return ErrNotFound
		}
	}
	return nil
}
