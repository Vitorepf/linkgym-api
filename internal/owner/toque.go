package owner

import (
	"context"
	"fmt"
	"strings"
)

// O TOQUE: o personal falou com alguém, e por qual motivo.
//
// É o que torna a fila falsificável. Sem ele, "assertivo" é superstição bem diagramada: o
// produto diz três nomes por dia e ninguém — nem nós — tem como perguntar se acertou. E é a
// única perda IRRECUPERÁVEL PARA TRÁS de todo este lote: o registro de que ele tocou em
// alguém hoje não existe depois do fato. Consertar isto em janeiro não devolve setembro.
//
// Gravado ao ABRIR o WhatsApp, nunca ao enviar: o app não tem como saber se a mensagem foi.
// A alternativa a gravar cedo é não gravar nada.
//
// Sem coluna de score, porque não existe score. O motivo é o SINAL que colocou a pessoa na
// fila (risco.go), então daqui a alguns meses dá para perguntar qual sinal de fato antecedeu
// uma saída — que é a única forma honesta de calibrar a fila.
func (s *Service) RegistrarToque(ctx context.Context, ownerID, personID, motivo string) error {
	motivo = strings.TrimSpace(motivo)
	if personID == "" || motivo == "" || len(motivo) > 60 {
		return ErrInvalid
	}
	studioID, err := s.ownerTime(ctx, ownerID)
	if err != nil {
		return err
	}
	// O INSERT confere o vínculo na própria escrita: tocar em quem não é do time não grava
	// e não revela que a pessoa existe.
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO toques (studio_id, person_id, motivo)
		SELECT b.studio_id, b.person_id, $3
		FROM bonds b
		WHERE b.person_id = $2 AND b.studio_id = $1 AND b.role = 'student'`,
		studioID, personID, motivo,
	)
	if err != nil {
		return fmt.Errorf("registrar toque: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// O PLACAR — a única estatística agregada do produto, e ela é sobre o comportamento DELE,
// não sobre a turma: "você tocou em 7 pessoas este mês, 5 voltaram a treinar".
//
// É o oposto do relatório de percentuais que a doutrina da casa condena: não classifica
// ninguém em faixa, não pontua aluno, e a frase só existe quando ele fez alguma coisa.
type Placar struct {
	Tocados  int `json:"tocados"`
	Voltaram int `json:"voltaram"`
}

func (s *Service) PlacarDoMes(ctx context.Context, studioID, mes string) (Placar, error) {
	var p Placar
	// "Voltou" é ter treinado DEPOIS do toque. A janela é o mês, e a comparação é por
	// pessoa: quem foi tocado duas vezes conta uma.
	err := s.db.QueryRowContext(ctx, `
		SELECT count(DISTINCT t.person_id),
		       count(DISTINCT t.person_id) FILTER (
		           WHERE EXISTS (
		               SELECT 1 FROM workout_sessions ws
		               WHERE ws.person_id = t.person_id
		                 AND ws.studio_id = t.studio_id
		                 AND ws.finished_at > t.created_at))
		FROM toques t
		WHERE t.studio_id = $1 AND t.created_at >= $2::date`,
		studioID, mes,
	).Scan(&p.Tocados, &p.Voltaram)
	if err != nil {
		return p, fmt.Errorf("placar: %w", err)
	}
	return p, nil
}
