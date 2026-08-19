package today

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"time"
)

type Service struct {
	db  *sql.DB
	now func() time.Time
}

func New(db *sql.DB, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{db: db, now: now}
}

type Payload struct {
	Time         Time          `json:"time"`
	Person       Person        `json:"person"`
	Prontidao    Prontidao     `json:"prontidao"`
	Ofensiva     Ofensiva      `json:"ofensiva"`
	XPTotal      int           `json:"xp_total"`
	Prescription *Prescription `json:"prescription"`
	Banner       *Banner       `json:"banner"`

	// A frase que o PERSONAL escreveu ao publicar a Prescricao de hoje, literal. Vazia
	// quando ele nao escreveu nada — e vazia significa que a tela nao desenha nada: o
	// produto nao tem frase de reserva para por na boca de outra pessoa.
	CoachLine string    `json:"coach_line"`
	Debut     bool      `json:"debut"`
	Comeback  *Comeback `json:"comeback"`

	// Cumprimento: a Sessao prescrita de HOJE fechada. Nunca volume, nunca carga, nunca
	// percentual do prescrito — ver o verbete no CONTEXT.md. E o unico ato que fecha o dia
	// da Ofensiva, e a tela precisa dele para acender o contador sem custar rolagem.
	//
	// Sem ficha publicada hoje nao ha o que cumprir, e o campo e false.
	Cumprido bool `json:"cumprido"`
}

type Time struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	AccentColor string `json:"accent_color"`
}

type Person struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Prontidao struct {
	Score    int    `json:"score"`
	Energy   int    `json:"energy"`
	Soreness int    `json:"soreness"`
	Sleep    int    `json:"sleep"`
	Label    string `json:"label"`
}

type Ofensiva struct {
	CurrentCount       int  `json:"current_count"`
	ProtectorAvailable bool `json:"protector_available"`
}

type Prescription struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	ForDate string `json:"for_date"`
	Minutes int    `json:"minutes"`
	Items   []Item `json:"items"`
}

type Item struct {
	ID          string  `json:"id"`
	ExerciseID  string  `json:"exercise_id"`
	Name        string  `json:"name"`
	Position    int     `json:"position"`
	PlannedSets int     `json:"planned_sets"`
	PlannedReps string  `json:"planned_reps"`
	LoadKg      float64 `json:"load_kg"`
	RestSeconds *int    `json:"rest_seconds"`
	Notes       *string `json:"notes"`
	VideoURL    *string `json:"video_url"`

	// O que o CORPO dele fez da ultima vez neste exercicio, para a tela ter contra o que
	// comparar a carga de hoje. Nao e a carga prescrita: sai de workout_sets, a serie
	// executada. Quem manda no campo continua sendo a Prescricao — o personal decide a
	// carga —, e isto entra ao lado, como referencia.
	//
	// Ponteiro porque a ausencia precisa ser dizivel: primeira vez naquele exercicio nao
	// tem anterior, e a tela desenha o traco em vez de inventar numero.
	LastKg   *float64 `json:"last_kg"`
	LastReps *int     `json:"last_reps"`
}

type Banner struct {
	Text string `json:"text"`
	Kind string `json:"kind"`
}

type Comeback struct {
	ID        string `json:"id"`
	Minutes   int    `json:"minutes"`
	CoachLine string `json:"coach_line"`
}

func (s *Service) Today(ctx context.Context, personID string) (*Payload, error) {
	now := s.now()
	day := now.Format("2006-01-02")

	var (
		out    Payload
		bondID string
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT p.id, p.name,
		       s.id, s.name, s.accent_color,
		       b.id
		FROM people p
		JOIN bonds b ON b.id = p.active_bond_id
		JOIN studios s ON s.id = b.studio_id
		WHERE p.id = $1`,
		personID,
	).Scan(
		&out.Person.ID, &out.Person.Name,
		&out.Time.ID, &out.Time.Name, &out.Time.AccentColor,
		&bondID,
	)
	if err != nil {
		return nil, fmt.Errorf("today person: %w", err)
	}

	if err := s.applyYesterdayMiss(ctx, personID, out.Time.ID, bondID, now); err != nil {
		return nil, err
	}
	if err := s.openD11Comeback(ctx, personID, out.Time.ID, bondID, now); err != nil {
		return nil, err
	}

	err = s.db.QueryRowContext(ctx, `
		SELECT COALESCE(st.current_count, 0), COALESCE(st.protector_available, true)
		FROM people p
		LEFT JOIN streaks st ON st.bond_id = p.active_bond_id
		WHERE p.id = $1`,
		personID,
	).Scan(&out.Ofensiva.CurrentCount, &out.Ofensiva.ProtectorAvailable)
	if err != nil {
		return nil, fmt.Errorf("today ofensiva: %w", err)
	}

	out.Prontidao = Prontidao{Score: 0, Label: "Como você está?"}
	var energy, soreness, sleep sql.NullInt64
	err = s.db.QueryRowContext(ctx, `
		SELECT energy, soreness, sleep
		FROM readiness_logs
		WHERE person_id = $1 AND for_date = $2`,
		personID, day,
	).Scan(&energy, &soreness, &sleep)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("today prontidao: %w", err)
	}
	if err == nil && energy.Valid && soreness.Valid && sleep.Valid {
		out.Prontidao = prontidaoFrom(int(energy.Int64), int(soreness.Int64), int(sleep.Int64))
	}

	if err := s.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(amount), 0) FROM xp_ledger WHERE bond_id = $1`,
		bondID,
	).Scan(&out.XPTotal); err != nil {
		return nil, fmt.Errorf("today xp: %w", err)
	}

	var finished int
	if err := s.db.QueryRowContext(ctx, `
		SELECT count(*) FROM workout_sessions
		WHERE person_id = $1 AND studio_id = $2 AND finished_at IS NOT NULL`,
		personID, out.Time.ID,
	).Scan(&finished); err != nil {
		return nil, fmt.Errorf("today debut: %w", err)
	}
	out.Debut = finished == 0

	out.Comeback, err = s.loadComeback(ctx, bondID)
	if err != nil {
		return nil, err
	}

	// Calculado ANTES da consulta da ficha, porque aquela sai cedo com ErrNoRows quando
	// nao ha prescricao hoje — e o campo tem que estar preenchido nos dois caminhos.
	if err := s.db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM workout_sessions ws
			JOIN prescriptions pr ON pr.id = ws.prescription_id
			WHERE pr.person_id = $1 AND pr.studio_id = $2 AND pr.for_date = $3
			  AND pr.status = 'published' AND ws.finished_at IS NOT NULL
		)`,
		personID, out.Time.ID, day,
	).Scan(&out.Cumprido); err != nil {
		return nil, fmt.Errorf("today cumprido: %w", err)
	}

	var (
		prID   string
		prName string
		prDate string
		frase  sql.NullString
	)
	err = s.db.QueryRowContext(ctx, `
		SELECT pr.id::text, m.name, pr.for_date::text, pr.coach_line
		FROM prescriptions pr
		JOIN models m ON m.id = pr.model_id
		WHERE pr.person_id = $1 AND pr.studio_id = $2
		  AND pr.for_date = $3 AND pr.status = 'published'`,
		personID, out.Time.ID, day,
	).Scan(&prID, &prName, &prDate, &frase)
	if err == sql.ErrNoRows {
		return &out, nil
	}
	if err != nil {
		return nil, fmt.Errorf("today prescription: %w", err)
	}

	items, err := s.loadItems(ctx, prID, personID)
	if err != nil {
		return nil, err
	}
	out.Prescription = &Prescription{
		ID:      prID,
		Name:    prName,
		ForDate: prDate,
		Minutes: 52,
		Items:   items,
	}
	out.Banner = &Banner{
		Text: fmt.Sprintf("%s publicou o %s", out.Time.Name, prName),
		Kind: "published",
	}
	// A frase do personal, LITERAL, ou nada.
	//
	// Aqui morava um template: "<exercicio> em <carga>. Tecnica, nao ego." — uma frase de
	// produto que a tela do aluno desenhava embaixo do rosto e do nome de quem nao a
	// escreveu. O campo agora e o texto que ele digitou ao publicar (prescriptions.
	// coach_line), e sem texto o campo volta vazio: a tela nao tem o que desenhar, e e
	// exatamente esse o conserto. Nao ha frase de reserva, porque frase de reserva com o
	// nome dele em cima e o defeito, nao a saida dele.
	out.CoachLine = frase.String
	return &out, nil
}

func (s *Service) loadItems(ctx context.Context, prescriptionID, personID string) ([]Item, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT pi.id::text, pi.exercise_id::text, e.name, pi.position,
		       pi.planned_sets, pi.planned_reps, pi.load_kg, pi.rest_seconds,
		       pi.notes, e.video_object_key
		FROM prescription_items pi
		JOIN exercises e ON e.id = pi.exercise_id
		WHERE pi.prescription_id = $1
		ORDER BY pi.position`,
		prescriptionID,
	)
	if err != nil {
		return nil, fmt.Errorf("today items: %w", err)
	}
	defer rows.Close()

	var items []Item
	for rows.Next() {
		var (
			it    Item
			load  sql.NullFloat64
			rest  sql.NullInt64
			notes sql.NullString
			video sql.NullString
		)
		if err := rows.Scan(
			&it.ID, &it.ExerciseID, &it.Name, &it.Position,
			&it.PlannedSets, &it.PlannedReps, &load, &rest,
			&notes, &video,
		); err != nil {
			return nil, fmt.Errorf("today item scan: %w", err)
		}
		it.LoadKg = load.Float64
		if rest.Valid {
			v := int(rest.Int64)
			it.RestSeconds = &v
		}
		if notes.Valid {
			n := notes.String
			it.Notes = &n
		}
		if video.Valid && video.String != "" {
			u := video.String
			it.VideoURL = &u
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := s.attachLast(ctx, personID, items); err != nil {
		return nil, err
	}
	return items, nil
}

// attachLast pendura, em cada item, a ultima serie EXECUTADA daquele exercicio por aquela
// pessoa. Uma consulta so para a ficha inteira: DISTINCT ON devolve a linha mais recente
// por exercicio, e o indice de workout_sets ja serve a ordenacao.
//
// `finished_at IS NOT NULL` e o que separa "a ultima vez" de "agora": a sessao de hoje,
// enquanto aberta, tem finished_at nulo e por isso nao se conta como referencia de si
// mesma. Dentro da sessao, quem herda a carga da serie anterior e o proprio app.
func (s *Service) attachLast(ctx context.Context, personID string, items []Item) error {
	if len(items) == 0 {
		return nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT ON (ws.exercise_id) ws.exercise_id::text, ws.load_kg, ws.reps
		FROM workout_sets ws
		JOIN workout_sessions sess ON sess.id = ws.session_id
		WHERE sess.person_id = $1
		  AND sess.finished_at IS NOT NULL
		  AND ws.load_kg IS NOT NULL
		ORDER BY ws.exercise_id, ws.performed_at DESC`,
		personID,
	)
	if err != nil {
		return fmt.Errorf("today last sets: %w", err)
	}
	defer rows.Close()

	type ultima struct {
		kg   float64
		reps sql.NullInt64
	}
	porExercicio := map[string]ultima{}
	for rows.Next() {
		var (
			id string
			u  ultima
		)
		if err := rows.Scan(&id, &u.kg, &u.reps); err != nil {
			return fmt.Errorf("today last sets scan: %w", err)
		}
		porExercicio[id] = u
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for i := range items {
		u, ok := porExercicio[items[i].ExerciseID]
		if !ok {
			continue
		}
		kg := u.kg
		items[i].LastKg = &kg
		if u.reps.Valid {
			r := int(u.reps.Int64)
			items[i].LastReps = &r
		}
	}
	return nil
}

var (
	ErrProntidaoInvalid = errors.New("invalido")
	ErrNotFound         = errors.New("nao_encontrado")
)

// A linha da Retomada diz o que CONTINUA DE PE, e so isso.
//
// Ela dizia "Sem culpa. Nove minutos e voce esta de volta." e tinha dois defeitos. O
// primeiro: nomear a culpa para nega-la ainda planta a palavra, e a barra do eixo 3
// (docs/barra/eixo3-ritual-aluno/) mostra o oposto — no Duolingo o dia perdido e igual ao
// dia futuro, sem X, sem vermelho e sem mencao a falha. O segundo: "Nove minutos" repetia
// em prosa o campo Minutes, que a tela ja desenha como "9 MIN" no proprio botao; bastava
// Minutes mudar para a frase mentir.
const comebackCoachLine = "Sua carga e seus recordes continuam aí."

func (s *Service) applyYesterdayMiss(ctx context.Context, personID, studioID, bondID string, now time.Time) error {
	today := now.Format("2006-01-02")
	yesterday := now.AddDate(0, 0, -1).Format("2006-01-02")

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var published int
	if err := tx.QueryRowContext(ctx, `
		SELECT count(*) FROM prescriptions
		WHERE person_id = $1 AND studio_id = $2 AND for_date = $3 AND status = 'published'`,
		personID, studioID, yesterday,
	).Scan(&published); err != nil {
		return fmt.Errorf("today miss published: %w", err)
	}
	if published == 0 {
		return nil
	}

	var finished int
	if err := tx.QueryRowContext(ctx, `
		SELECT count(*) FROM workout_sessions ws
		JOIN prescriptions pr ON pr.id = ws.prescription_id
		WHERE pr.person_id = $1 AND pr.studio_id = $2 AND pr.for_date = $3
		  AND pr.status = 'published' AND ws.finished_at IS NOT NULL`,
		personID, studioID, yesterday,
	).Scan(&finished); err != nil {
		return fmt.Errorf("today miss session: %w", err)
	}
	if finished > 0 {
		return nil
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO streaks (bond_id, current_count, protector_available)
		VALUES ($1, 0, true)
		ON CONFLICT (bond_id) DO NOTHING`,
		bondID,
	); err != nil {
		return fmt.Errorf("today miss ofensiva insert: %w", err)
	}

	var (
		count        int
		protector    bool
		spentToday   bool
		updatedToday bool
	)
	if err := tx.QueryRowContext(ctx, `
		SELECT current_count, protector_available,
		       COALESCE(protector_spent_at::date = $2::date, false),
		       updated_at::date = $2::date
		FROM streaks WHERE bond_id = $1
		FOR UPDATE`,
		bondID, today,
	).Scan(&count, &protector, &spentToday, &updatedToday); err != nil {
		return fmt.Errorf("today miss ofensiva: %w", err)
	}
	if spentToday || (count == 0 && !protector && updatedToday) {
		return nil
	}

	if protector {
		if _, err := tx.ExecContext(ctx, `
			UPDATE streaks
			SET protector_available = false, protector_spent_at = $2, updated_at = $2
			WHERE bond_id = $1`,
			bondID, now,
		); err != nil {
			return fmt.Errorf("today miss spend: %w", err)
		}
	} else {
		if _, err := tx.ExecContext(ctx, `
			UPDATE streaks
			SET current_count = 0, updated_at = $2
			WHERE bond_id = $1`,
			bondID, now,
		); err != nil {
			return fmt.Errorf("today miss zero: %w", err)
		}
	}

	// A Retomada nasce AQUI, na primeira falta, dentro do mesmo commit que mexe na
	// Ofensiva. Antes o unico caminho automatico era openD11Comeback, que so abre depois de
	// ONZE dias de silencio (`calendarDays(anchor, now) < 11`); fora dele, so nascia quando
	// o personal aplicava um student_stopped da fila. Ou seja: quem faltou ontem nao via
	// nada, e a volta dependia de outra pessoa agir.
	//
	// missedOn e ONTEM, o dia que a prescricao ficou sem sessao — e o UNIQUE
	// (bond_id, missed_on) da tabela e o que impede a mesma falta de abrir duas.
	if err := upsertOpenComeback(ctx, tx, bondID, yesterday); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) openD11Comeback(ctx context.Context, personID, studioID, bondID string, now time.Time) error {
	var lastFulfilled sql.NullTime
	err := s.db.QueryRowContext(ctx, `
		SELECT last_fulfilled_on FROM streaks WHERE bond_id = $1`,
		bondID,
	).Scan(&lastFulfilled)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return fmt.Errorf("today d11 ofensiva: %w", err)
	}

	anchor := lastFulfilled
	if !anchor.Valid {
		err = s.db.QueryRowContext(ctx, `
			SELECT MAX(finished_at) FROM workout_sessions
			WHERE person_id = $1 AND studio_id = $2 AND finished_at IS NOT NULL`,
			personID, studioID,
		).Scan(&anchor)
		if err != nil {
			return fmt.Errorf("today d11 finished: %w", err)
		}
	}
	if !anchor.Valid {
		return nil
	}

	// MARCO DE CALENDARIO, nao dia arbitrario.
	//
	// Era `calendarDays(anchor, now) < 11`: o convite de volta caia no decimo primeiro dia
	// de silencio, que nao e nada para quem recebe — cai numa quarta-feira qualquer, no meio
	// da tarde. Marco e dia que a pessoa ja trata como recomeco, e por isso o convite pega.
	//
	// Marcos que sao computaveis SO da data, sem dado novo e sem dependencia nova: segunda
	// e dia 1º. Feriado e aniversario tambem sao marcos e ficam de fora por falta de dado —
	// nao existe data de nascimento em `people` nem tabela de feriados, e inventar
	// qualquer um dos dois seria marco falso. Declarado aqui em vez de silenciado.
	if calendarDays(anchor.Time, now) < 7 || !marcoDeCalendario(now) {
		return nil
	}

	return upsertOpenComeback(ctx, s.db, bondID, now.Format("2006-01-02"))
}

// marcoDeCalendario diz se o dia e um recomeco que a pessoa ja reconhece sozinha.
func marcoDeCalendario(t time.Time) bool {
	return t.Weekday() == time.Monday || t.Day() == 1
}

func calendarDays(from, to time.Time) int {
	a := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, time.UTC)
	b := time.Date(to.Year(), to.Month(), to.Day(), 0, 0, 0, 0, time.UTC)
	return int(b.Sub(a).Hours() / 24)
}

func upsertOpenComeback(ctx context.Context, db execQuerier, bondID, missedOn string) error {
	var openID sql.NullString
	err := db.QueryRowContext(ctx, `
		SELECT id::text FROM comebacks
		WHERE bond_id = $1 AND completed_at IS NULL
		LIMIT 1`,
		bondID,
	).Scan(&openID)
	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("today comeback open: %w", err)
	}
	if openID.Valid {
		return nil
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO comebacks (bond_id, missed_on)
		VALUES ($1, $2::date)
		ON CONFLICT (bond_id, missed_on) DO NOTHING`,
		bondID, missedOn,
	); err != nil {
		return fmt.Errorf("today comeback insert: %w", err)
	}
	return nil
}

type execQuerier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func (s *Service) loadComeback(ctx context.Context, bondID string) (*Comeback, error) {
	var id string
	err := s.db.QueryRowContext(ctx, `
		SELECT id::text FROM comebacks
		WHERE bond_id = $1 AND completed_at IS NULL
		ORDER BY missed_on DESC
		LIMIT 1`,
		bondID,
	).Scan(&id)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("today comeback: %w", err)
	}
	return &Comeback{
		ID:        id,
		Minutes:   9,
		CoachLine: comebackCoachLine,
	}, nil
}

func (s *Service) CompleteComeback(ctx context.Context, personID, comebackID string) error {
	if comebackID == "" {
		return ErrNotFound
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var studioID string
	err = tx.QueryRowContext(ctx, `
		UPDATE comebacks c
		SET completed_at = COALESCE(c.completed_at, $3)
		FROM people p, bonds b
		WHERE c.id = $1 AND p.id = $2
		  AND c.bond_id = p.active_bond_id
		  AND b.id = c.bond_id
		RETURNING b.studio_id::text`,
		comebackID, personID, s.now(),
	).Scan(&studioID)
	if err == sql.ErrNoRows {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("today comeback complete: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO badges (studio_id, person_id, badge_key)
		VALUES ($1, $2, 'retomada')
		ON CONFLICT (studio_id, person_id, badge_key) DO NOTHING`,
		studioID, personID,
	); err != nil {
		return fmt.Errorf("today retomada badge: %w", err)
	}

	return tx.Commit()
}

func (s *Service) PutProntidao(ctx context.Context, personID string, energy, soreness, sleep int) (Prontidao, error) {
	if !inScale(energy) || !inScale(soreness) || !inScale(sleep) {
		return Prontidao{}, ErrProntidaoInvalid
	}
	day := s.now().Format("2006-01-02")
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO readiness_logs (person_id, for_date, energy, soreness, sleep)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (person_id, for_date)
		DO UPDATE SET energy = EXCLUDED.energy,
		              soreness = EXCLUDED.soreness,
		              sleep = EXCLUDED.sleep`,
		personID, day, energy, soreness, sleep,
	)
	if err != nil {
		return Prontidao{}, fmt.Errorf("put prontidao: %w", err)
	}
	return prontidaoFrom(energy, soreness, sleep), nil
}

func inScale(n int) bool {
	return n >= 1 && n <= 5
}

func prontidaoFrom(energy, soreness, sleep int) Prontidao {
	n := score(energy, soreness, sleep)
	return Prontidao{
		Score:    n,
		Energy:   energy,
		Soreness: soreness,
		Sleep:    sleep,
		Label:    label(n),
	}
}

func score(energy, soreness, sleep int) int {
	return int(math.Round(float64(energy+(6-soreness)+sleep) / 15.0 * 100))
}

func label(n int) string {
	if n >= 70 {
		return "Pode ir com carga"
	}
	if n >= 40 {
		return "Hoje não é dia de PR"
	}
	return "Versão leve"
}
