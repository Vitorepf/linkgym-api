package owner

import (
	"context"
	"fmt"
)

// A SAÍDA, que nunca foi escrita.
//
// `bonds.status` aceita 'invited', 'active', 'paused' e 'ended' desde a migration 00001, e
// um grep no repositório inteiro não acha UMA linha de Go que escreva 'paused' ou 'ended':
// o aceite do convite grava 'active' e ninguém nunca grava mais nada. Toda consulta do
// produto filtra `status = 'active'` — o filtro existe, o estado que ele filtra nunca chega.
//
// A consequência não é cosmética e é CUMULATIVA. O personal perde três alunos em setembro
// (mudaram de cidade). Em 1º de outubro a Operação abre com os 28 de sempre, a receita
// combinada inteira, o ticket médio inflado, e três linhas em aberto de gente que nunca
// mais vai pagar. Em novembro são seis, em dezembro nove. A lista "em aberto" vira um
// cemitério, e a única saída que a tela oferece é mentir: tocar "Recebi" em quem não pagou,
// todo mês, para limpar a lista. A partir daí NENHUM número da tela é verdade.
//
// E há a consequência futura: sem evento de saída não existe rótulo, e sem rótulo nenhuma
// afirmação sobre risco de cancelamento é falsificável — o produto não teria como aprender
// se a fila dele acerta.
//
// PAUSAR é diferente de ENCERRAR e as duas existem porque a vida do personal tem as duas:
// quem viajou dois meses volta, e quem mudou de cidade não. Pausado sai das contas do mês
// (não deve, não conta receita) e continua no cadastro; encerrado sai de tudo.
type EstadoDoVinculo string

const (
	VinculoAtivo     EstadoDoVinculo = "active"
	VinculoPausado   EstadoDoVinculo = "paused"
	VinculoEncerrado EstadoDoVinculo = "ended"
)

// MudarEstadoDoVinculo é reversível de propósito: encerrar por engano não pode ser uma
// porta de mão única, e "voltei a treinar com o Fred" é um acontecimento comum. O histórico
// de sessões, cargas e pagamentos fica intacto em qualquer transição — nada é apagado, só
// o vínculo muda de estado.
func (s *Service) MudarEstadoDoVinculo(ctx context.Context, ownerID, bondID string, estado EstadoDoVinculo) error {
	switch estado {
	case VinculoAtivo, VinculoPausado, VinculoEncerrado:
	default:
		return ErrInvalid
	}
	studioID, err := s.ownerTime(ctx, ownerID)
	if err != nil {
		return err
	}
	// `role = 'student'` na cláusula: o dono não pode encerrar o próprio vínculo de owner e
	// ficar sem estúdio — e o índice único de um owner por estúdio tornaria isso um estado
	// de que não há volta pela interface.
	res, err := s.db.ExecContext(ctx, `
		UPDATE bonds SET status = $3, updated_at = now()
		WHERE id = $1 AND studio_id = $2 AND role = 'student'`,
		bondID, studioID, string(estado),
	)
	if err != nil {
		return fmt.Errorf("mudar estado do vinculo: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
