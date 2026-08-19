package owner

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/Vitorepf/linkgym-api/internal/publish"
)

type WeekItem struct {
	PersonID  string `json:"person_id"`
	Name      string `json:"name"`
	Adherence string `json:"adherence"`
	Suggested string `json:"suggested"`
	Selected  bool   `json:"selected"`
}

type LastLoad struct {
	ExerciseName string  `json:"exercise_name"`
	LoadKg       float64 `json:"load_kg"`
}

type Combinado struct {
	AmountCents int `json:"amount_cents"`
	DueDay      int `json:"due_day"`
}

type StudentCard struct {
	PersonID   string     `json:"person_id"`
	BondID     string     `json:"bond_id"`
	Name       string     `json:"name"`
	LastEffort *int       `json:"last_effort"`
	LastLoads  []LastLoad `json:"last_loads"`
	Ofensiva   struct {
		CurrentCount int `json:"current_count"`
	} `json:"ofensiva"`
	Suggested      string  `json:"suggested"`
	CommitmentText *string `json:"commitment_text"`
	// O combinado desta pessoa. NULL quando o personal nunca digitou — e é essa ausência
	// que a tela usa para oferecer "Combinar o valor" em vez de mostrar um R$ 0 mentiroso.
	Combinado *Combinado `json:"combinado"`
}

func (s *Service) Week(ctx context.Context, ownerID, from string) ([]WeekItem, error) {
	studioID, err := s.ownerTime(ctx, ownerID)
	if err != nil {
		return nil, err
	}

	start, err := weekStart(s.now(), from)
	if err != nil {
		return nil, err
	}
	end := start.AddDate(0, 0, 6)

	rows, err := s.db.QueryContext(ctx, `
		SELECT p.id::text, p.name,
		       COALESCE((b.onboarding->>'days_per_week')::int, 3),
		       (SELECT count(*) FROM fio_days f
		        WHERE f.person_id = p.id AND f.studio_id = b.studio_id
		          AND f.for_date BETWEEN $2 AND $3 AND f.status = 'done'),
		       (SELECT count(*) FROM fio_days f
		        WHERE f.person_id = p.id AND f.studio_id = b.studio_id
		          AND f.for_date BETWEEN $2 AND $3 AND f.status = 'missing'),
		       (SELECT ws.effort FROM workout_sessions ws
		        WHERE ws.person_id = p.id AND ws.studio_id = b.studio_id
		          AND ws.finished_at IS NOT NULL
		        ORDER BY ws.finished_at DESC LIMIT 1)
		FROM bonds b
		JOIN people p ON p.id = b.person_id
		WHERE b.studio_id = $1 AND b.role = 'student' AND b.status = 'active'
		ORDER BY p.name`,
		studioID, start.Format("2006-01-02"), end.Format("2006-01-02"),
	)
	if err != nil {
		return nil, fmt.Errorf("owner week: %w", err)
	}
	defer rows.Close()

	items := []WeekItem{}
	for rows.Next() {
		var (
			it      WeekItem
			days    int
			done    int
			missing int
			effort  sql.NullFloat64
		)
		if err := rows.Scan(&it.PersonID, &it.Name, &days, &done, &missing, &effort); err != nil {
			return nil, fmt.Errorf("owner week scan: %w", err)
		}
		if days <= 0 {
			days = 3
		}
		it.Adherence = fmt.Sprintf("%d/%d", done, days)
		it.Suggested = weekSuggested(effort, missing)
		it.Selected = true
		items = append(items, it)
	}
	return items, rows.Err()
}

func weekSuggested(effort sql.NullFloat64, missing int) string {
	if effort.Valid {
		switch int(effort.Float64) {
		case 1:
			return "+2,5 kg no supino"
		case 3:
			return "manter"
		}
	}
	if missing >= 2 {
		return "versão curta"
	}
	return "manter"
}

func weekStart(now time.Time, from string) (time.Time, error) {
	if from == "" {
		mon, _ := isoWeekBounds(now)
		return mon, nil
	}
	t, err := time.Parse("2006-01-02", from)
	if err != nil {
		return time.Time{}, ErrInvalid
	}
	mon, _ := isoWeekBounds(t)
	return mon, nil
}

func (s *Service) Approve(ctx context.Context, ownerID string, personIDs []string) (int, error) {
	studioID, err := s.ownerTime(ctx, ownerID)
	if err != nil {
		return 0, err
	}
	now := s.now()
	tomorrow := now.AddDate(0, 0, 1).Format("2006-01-02")

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	count := 0
	for _, personID := range personIDs {
		if personID == "" {
			continue
		}
		published, err := publishTomorrowFromModelo(ctx, tx, studioID, personID, tomorrow, now)
		if err != nil {
			if err == ErrNotFound {
				continue
			}
			return 0, err
		}
		if published {
			count++
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return count, nil
}

func publishTomorrowFromModelo(ctx context.Context, tx *sql.Tx, studioID, personID, tomorrow string, now time.Time) (bool, error) {
	var n int
	if err := tx.QueryRowContext(ctx, `
		SELECT count(*) FROM bonds
		WHERE person_id = $1 AND studio_id = $2 AND role = 'student' AND status = 'active'`,
		personID, studioID,
	).Scan(&n); err != nil {
		return false, fmt.Errorf("owner week bond: %w", err)
	}
	if n == 0 {
		return false, ErrNotFound
	}

	var existing int
	if err := tx.QueryRowContext(ctx, `
		SELECT count(*) FROM prescriptions
		WHERE person_id = $1 AND studio_id = $2 AND for_date = $3::date AND status = 'published'`,
		personID, studioID, tomorrow,
	).Scan(&existing); err != nil {
		return false, fmt.Errorf("owner week existing: %w", err)
	}
	if existing > 0 {
		return false, nil
	}

	var modelID string
	err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(
			(SELECT model_id::text FROM prescriptions
			 WHERE person_id = $1 AND studio_id = $2 AND status = 'published'
			 ORDER BY for_date DESC LIMIT 1),
			(SELECT id::text FROM models WHERE studio_id = $2 AND name = 'Treino A' LIMIT 1)
		)`,
		personID, studioID,
	).Scan(&modelID)
	if err != nil || modelID == "" {
		if err == sql.ErrNoRows || modelID == "" {
			return false, ErrNotFound
		}
		return false, fmt.Errorf("owner week model: %w", err)
	}

	var prID string
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO prescriptions (studio_id, person_id, model_id, for_date, status, published_at)
		VALUES ($1, $2, $3::uuid, $4::date, 'published', $5)
		RETURNING id::text`,
		studioID, personID, modelID, tomorrow, now,
	).Scan(&prID); err != nil {
		return false, fmt.Errorf("owner week prescription: %w", err)
	}

	if _, err := tx.ExecContext(ctx, publish.ItensComCargaDoCorpo,
		prID, personID, studioID, tomorrow, modelID,
	); err != nil {
		return false, fmt.Errorf("owner week items: %w", err)
	}
	return true, nil
}

func (s *Service) Student(ctx context.Context, ownerID, personID string) (*StudentCard, error) {
	if personID == "" {
		return nil, ErrNotFound
	}
	studioID, err := s.ownerTime(ctx, ownerID)
	if err != nil {
		return nil, err
	}

	var (
		out        StudentCard
		bondID     string
		reason     sql.NullString
		published  int
		openCome   sql.NullString
		commitment sql.NullString
	)
	err = s.db.QueryRowContext(ctx, `
		SELECT p.id::text, p.name, b.id::text, b.commitment_text,
		       (SELECT a.reason FROM attention_items a
		        WHERE a.person_id = p.id AND a.studio_id = b.studio_id AND a.for_date = $3::date
		        LIMIT 1),
		       (SELECT count(*) FROM prescriptions pr
		        WHERE pr.person_id = p.id AND pr.studio_id = b.studio_id AND pr.status = 'published'),
		       (SELECT c.id::text FROM comebacks c
		        WHERE c.bond_id = b.id AND c.completed_at IS NULL LIMIT 1)
		FROM people p
		JOIN bonds b ON b.person_id = p.id AND b.studio_id = $2
		WHERE p.id = $1 AND b.role = 'student' AND b.status = 'active'`,
		personID, studioID, s.now().Format("2006-01-02"),
	).Scan(&out.PersonID, &out.Name, &bondID, &commitment, &reason, &published, &openCome)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("owner student: %w", err)
	}
	out.BondID = bondID

	// O combinado. Ausente é ausente: sem linha em `mensalidades`, o ponteiro fica nil e a
	// tela oferece combinar em vez de desenhar R$ 0 — que seria o app afirmando que esta
	// pessoa treina de graça.
	var comb Combinado
	err = s.db.QueryRowContext(ctx, `
		SELECT amount_cents, due_day FROM mensalidades WHERE bond_id = $1`,
		bondID,
	).Scan(&comb.AmountCents, &comb.DueDay)
	if err == nil {
		out.Combinado = &comb
	} else if err != sql.ErrNoRows {
		return nil, fmt.Errorf("owner student combinado: %w", err)
	}

	if err := s.db.QueryRowContext(ctx, `
		SELECT COALESCE(current_count, 0) FROM streaks WHERE bond_id = $1`,
		bondID,
	).Scan(&out.Ofensiva.CurrentCount); err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("owner student ofensiva: %w", err)
	}

	var effort sql.NullFloat64
	err = s.db.QueryRowContext(ctx, `
		SELECT effort FROM workout_sessions
		WHERE person_id = $1 AND studio_id = $2 AND finished_at IS NOT NULL
		ORDER BY finished_at DESC LIMIT 1`,
		personID, studioID,
	).Scan(&effort)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("owner student effort: %w", err)
	}
	if effort.Valid {
		v := int(effort.Float64)
		out.LastEffort = &v
	}

	out.LastLoads = []LastLoad{}
	rows, err := s.db.QueryContext(ctx, `
		SELECT e.name, pi.load_kg
		FROM prescription_items pi
		JOIN exercises e ON e.id = pi.exercise_id
		WHERE pi.prescription_id = (
			SELECT id FROM prescriptions
			WHERE person_id = $1 AND studio_id = $2 AND status = 'published'
			ORDER BY for_date DESC LIMIT 1
		)
		ORDER BY pi.position`,
		personID, studioID,
	)
	if err != nil {
		return nil, fmt.Errorf("owner student loads: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var load LastLoad
		if err := rows.Scan(&load.ExerciseName, &load.LoadKg); err != nil {
			return nil, fmt.Errorf("owner student loads scan: %w", err)
		}
		out.LastLoads = append(out.LastLoads, load)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if commitment.Valid && commitment.String != "" {
		text := commitment.String
		out.CommitmentText = &text
	}

	out.Suggested = studentSuggested(reason.String, openCome.Valid, published > 0)
	return &out, nil
}

func studentSuggested(reason string, comeback bool, hasPublished bool) string {
	if reason == "student_stopped" || comeback {
		return "nudge"
	}
	if reason == "debut" || !hasPublished {
		return "renew"
	}
	return "keep"
}
