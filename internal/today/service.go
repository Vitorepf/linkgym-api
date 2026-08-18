package today

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strconv"
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
	Studio       Studio        `json:"studio"`
	Person       Person        `json:"person"`
	Readiness    Readiness     `json:"readiness"`
	Streak       Streak        `json:"streak"`
	XPTotal      int           `json:"xp_total"`
	Prescription *Prescription `json:"prescription"`
	Banner       *Banner       `json:"banner"`
	CoachLine    string        `json:"coach_line"`
	Debut        bool          `json:"debut"`
	Comeback     *Comeback     `json:"comeback"`
}

type Studio struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	AccentColor string `json:"accent_color"`
}

type Person struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Readiness struct {
	Score    int    `json:"score"`
	Energy   int    `json:"energy"`
	Soreness int    `json:"soreness"`
	Sleep    int    `json:"sleep"`
	Label    string `json:"label"`
}

type Streak struct {
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
		&out.Studio.ID, &out.Studio.Name, &out.Studio.AccentColor,
		&bondID,
	)
	if err != nil {
		return nil, fmt.Errorf("today person: %w", err)
	}

	if err := s.applyYesterdayMiss(ctx, personID, out.Studio.ID, bondID, now); err != nil {
		return nil, err
	}
	if err := s.openD11Comeback(ctx, personID, out.Studio.ID, bondID, now); err != nil {
		return nil, err
	}

	err = s.db.QueryRowContext(ctx, `
		SELECT COALESCE(st.current_count, 0), COALESCE(st.protector_available, true)
		FROM people p
		LEFT JOIN streaks st ON st.bond_id = p.active_bond_id
		WHERE p.id = $1`,
		personID,
	).Scan(&out.Streak.CurrentCount, &out.Streak.ProtectorAvailable)
	if err != nil {
		return nil, fmt.Errorf("today streak: %w", err)
	}

	out.Readiness = Readiness{Score: 0, Label: "Como você está?"}
	var energy, soreness, sleep sql.NullInt64
	err = s.db.QueryRowContext(ctx, `
		SELECT energy, soreness, sleep
		FROM readiness_logs
		WHERE person_id = $1 AND for_date = $2`,
		personID, day,
	).Scan(&energy, &soreness, &sleep)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("today readiness: %w", err)
	}
	if err == nil && energy.Valid && soreness.Valid && sleep.Valid {
		out.Readiness = readinessFrom(int(energy.Int64), int(soreness.Int64), int(sleep.Int64))
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
		personID, out.Studio.ID,
	).Scan(&finished); err != nil {
		return nil, fmt.Errorf("today debut: %w", err)
	}
	out.Debut = finished == 0

	out.Comeback, err = s.loadComeback(ctx, bondID)
	if err != nil {
		return nil, err
	}

	var (
		prID   string
		prName string
		prDate string
	)
	err = s.db.QueryRowContext(ctx, `
		SELECT pr.id::text, m.name, pr.for_date::text
		FROM prescriptions pr
		JOIN models m ON m.id = pr.model_id
		WHERE pr.person_id = $1 AND pr.studio_id = $2
		  AND pr.for_date = $3 AND pr.status = 'published'`,
		personID, out.Studio.ID, day,
	).Scan(&prID, &prName, &prDate)
	if err == sql.ErrNoRows {
		return &out, nil
	}
	if err != nil {
		return nil, fmt.Errorf("today prescription: %w", err)
	}

	items, err := s.loadItems(ctx, prID)
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
		Text: fmt.Sprintf("%s publicou o %s", out.Studio.Name, prName),
		Kind: "published",
	}
	if len(items) > 0 {
		out.CoachLine = fmt.Sprintf("%s em %s. Técnica, não ego.", items[0].Name, formatKg(items[0].LoadKg))
	}
	return &out, nil
}

func (s *Service) loadItems(ctx context.Context, prescriptionID string) ([]Item, error) {
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
	return items, rows.Err()
}

var (
	ErrReadinessInvalid = errors.New("invalido")
	ErrNotFound         = errors.New("nao_encontrado")
)

const comebackCoachLine = "Sem culpa. Nove minutos e você está de volta."

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
		return fmt.Errorf("today miss streak insert: %w", err)
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
		return fmt.Errorf("today miss streak: %w", err)
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
		return fmt.Errorf("today d11 streak: %w", err)
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

	if calendarDays(anchor.Time, now) < 11 {
		return nil
	}

	return upsertOpenComeback(ctx, s.db, bondID, now.Format("2006-01-02"))
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
	res, err := s.db.ExecContext(ctx, `
		UPDATE comebacks c
		SET completed_at = $3
		FROM people p
		WHERE c.id = $1 AND p.id = $2
		  AND c.bond_id = p.active_bond_id
		  AND c.completed_at IS NULL`,
		comebackID, personID, s.now(),
	)
	if err != nil {
		return fmt.Errorf("today comeback complete: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Service) PutReadiness(ctx context.Context, personID string, energy, soreness, sleep int) (Readiness, error) {
	if !inScale(energy) || !inScale(soreness) || !inScale(sleep) {
		return Readiness{}, ErrReadinessInvalid
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
		return Readiness{}, fmt.Errorf("put readiness: %w", err)
	}
	return readinessFrom(energy, soreness, sleep), nil
}

func inScale(n int) bool {
	return n >= 1 && n <= 5
}

func readinessFrom(energy, soreness, sleep int) Readiness {
	n := score(energy, soreness, sleep)
	return Readiness{
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

func formatKg(kg float64) string {
	if kg == float64(int64(kg)) {
		return strconv.FormatInt(int64(kg), 10)
	}
	return strconv.FormatFloat(kg, 'f', -1, 64)
}
