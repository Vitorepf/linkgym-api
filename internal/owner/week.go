package owner

import (
	"context"
	"database/sql"
	"fmt"
	"time"
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

type StudentCard struct {
	PersonID   string     `json:"person_id"`
	Name       string     `json:"name"`
	LastEffort *int       `json:"last_effort"`
	LastLoads  []LastLoad `json:"last_loads"`
	Streak     struct {
		CurrentCount int `json:"current_count"`
	} `json:"streak"`
	Suggested string `json:"suggested"`
}

func (s *Service) Week(ctx context.Context, ownerID, from string) ([]WeekItem, error) {
	studioID, err := s.ownerStudio(ctx, ownerID)
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
	studioID, err := s.ownerStudio(ctx, ownerID)
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
		if err := publishTomorrowFromModelo(ctx, tx, studioID, personID, tomorrow, now); err != nil {
			if err == ErrNotFound {
				continue
			}
			return 0, err
		}
		count++
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return count, nil
}

func publishTomorrowFromModelo(ctx context.Context, tx *sql.Tx, studioID, personID, tomorrow string, now time.Time) error {
	var n int
	if err := tx.QueryRowContext(ctx, `
		SELECT count(*) FROM bonds
		WHERE person_id = $1 AND studio_id = $2 AND role = 'student' AND status = 'active'`,
		personID, studioID,
	).Scan(&n); err != nil {
		return fmt.Errorf("owner week bond: %w", err)
	}
	if n == 0 {
		return ErrNotFound
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
			return ErrNotFound
		}
		return fmt.Errorf("owner week model: %w", err)
	}

	var prID string
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO prescriptions (studio_id, person_id, model_id, for_date, status, published_at)
		VALUES ($1, $2, $3::uuid, $4::date, 'published', $5)
		ON CONFLICT (person_id, studio_id, for_date) WHERE status = 'published'
		DO UPDATE SET
			model_id = EXCLUDED.model_id,
			published_at = EXCLUDED.published_at,
			updated_at = now()
		RETURNING id::text`,
		studioID, personID, modelID, tomorrow, now,
	).Scan(&prID); err != nil {
		return fmt.Errorf("owner week prescription: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		DELETE FROM prescription_items WHERE prescription_id = $1`,
		prID,
	); err != nil {
		return fmt.Errorf("owner week clear items: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO prescription_items (
			prescription_id, exercise_id, position, planned_sets, planned_reps,
			load_kg, rest_seconds, notes, load_source
		)
		SELECT $1, mi.exercise_id, mi.position, mi.planned_sets, mi.planned_reps,
		       COALESCE(hist.load_kg, mi.starter_load_kg),
		       mi.rest_seconds, mi.notes,
		       CASE WHEN hist.load_kg IS NOT NULL THEN 'history' ELSE 'starter' END
		FROM model_items mi
		LEFT JOIN LATERAL (
			SELECT pi.load_kg
			FROM prescriptions pr
			JOIN prescription_items pi
			  ON pi.prescription_id = pr.id AND pi.exercise_id = mi.exercise_id
			WHERE pr.person_id = $2 AND pr.studio_id = $3
			  AND pr.status = 'published' AND pr.for_date <> $4::date
			ORDER BY pr.for_date DESC
			LIMIT 1
		) hist ON true
		WHERE mi.model_id = $5::uuid`,
		prID, personID, studioID, tomorrow, modelID,
	); err != nil {
		return fmt.Errorf("owner week items: %w", err)
	}
	return nil
}

func (s *Service) Student(ctx context.Context, ownerID, personID string) (*StudentCard, error) {
	if personID == "" {
		return nil, ErrNotFound
	}
	studioID, err := s.ownerStudio(ctx, ownerID)
	if err != nil {
		return nil, err
	}

	var (
		out       StudentCard
		bondID    string
		reason    sql.NullString
		published int
		openCome  sql.NullString
	)
	err = s.db.QueryRowContext(ctx, `
		SELECT p.id::text, p.name, b.id::text,
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
	).Scan(&out.PersonID, &out.Name, &bondID, &reason, &published, &openCome)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("owner student: %w", err)
	}

	if err := s.db.QueryRowContext(ctx, `
		SELECT COALESCE(current_count, 0) FROM streaks WHERE bond_id = $1`,
		bondID,
	).Scan(&out.Streak.CurrentCount); err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("owner student streak: %w", err)
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
