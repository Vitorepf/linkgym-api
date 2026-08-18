package publish

import (
	"context"
	"database/sql"
	"fmt"
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

type Model struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type ModelDetail struct {
	ID    string      `json:"id"`
	Name  string      `json:"name"`
	Items []ModelItem `json:"items"`
}

type ModelItem struct {
	ID            string  `json:"id"`
	ExerciseID    string  `json:"exercise_id"`
	Name          string  `json:"name"`
	Position      int     `json:"position"`
	PlannedSets   int     `json:"planned_sets"`
	PlannedReps   string  `json:"planned_reps"`
	StarterLoadKg float64 `json:"starter_load_kg"`
}

type Draft struct {
	DraftID string      `json:"draft_id"`
	Items   []DraftItem `json:"items"`
}

type DraftItem struct {
	ID          string  `json:"id"`
	ExerciseID  string  `json:"exercise_id"`
	Name        string  `json:"name"`
	PlannedSets int     `json:"planned_sets"`
	PlannedReps string  `json:"planned_reps"`
	LoadKg      float64 `json:"load_kg"`
	LoadSource  string  `json:"load_source"`
}

type ItemPatch struct {
	LoadKg      float64 `json:"load_kg"`
	PlannedSets int     `json:"planned_sets"`
	PlannedReps string  `json:"planned_reps"`
}

func (s *Service) ownerTime(ctx context.Context, personID string) (string, error) {
	var role, studioID string
	err := s.db.QueryRowContext(ctx, `
		SELECT b.role, b.studio_id::text
		FROM people p
		JOIN bonds b ON b.id = p.active_bond_id
		WHERE p.id = $1`,
		personID,
	).Scan(&role, &studioID)
	if err == sql.ErrNoRows {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	if role != "owner" {
		return "", ErrForbidden
	}
	return studioID, nil
}

func (s *Service) ListModels(ctx context.Context, ownerID string) ([]Model, error) {
	studioID, err := s.ownerTime(ctx, ownerID)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id::text, name FROM models
		WHERE studio_id = $1
		ORDER BY name`,
		studioID,
	)
	if err != nil {
		return nil, fmt.Errorf("publish models: %w", err)
	}
	defer rows.Close()

	items := []Model{}
	for rows.Next() {
		var m Model
		if err := rows.Scan(&m.ID, &m.Name); err != nil {
			return nil, fmt.Errorf("publish models scan: %w", err)
		}
		items = append(items, m)
	}
	return items, rows.Err()
}

func (s *Service) GetModel(ctx context.Context, ownerID, modelID string) (*ModelDetail, error) {
	if modelID == "" {
		return nil, ErrNotFound
	}
	studioID, err := s.ownerTime(ctx, ownerID)
	if err != nil {
		return nil, err
	}

	var out ModelDetail
	err = s.db.QueryRowContext(ctx, `
		SELECT id::text, name FROM models
		WHERE id = $1 AND studio_id = $2`,
		modelID, studioID,
	).Scan(&out.ID, &out.Name)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("publish model: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT mi.id::text, mi.exercise_id::text, e.name, mi.position,
		       mi.planned_sets, mi.planned_reps, COALESCE(mi.starter_load_kg, 0)
		FROM model_items mi
		JOIN exercises e ON e.id = mi.exercise_id
		WHERE mi.model_id = $1
		ORDER BY mi.position`,
		modelID,
	)
	if err != nil {
		return nil, fmt.Errorf("publish model items: %w", err)
	}
	defer rows.Close()

	out.Items = []ModelItem{}
	for rows.Next() {
		var it ModelItem
		if err := rows.Scan(&it.ID, &it.ExerciseID, &it.Name, &it.Position, &it.PlannedSets, &it.PlannedReps, &it.StarterLoadKg); err != nil {
			return nil, fmt.Errorf("publish model items scan: %w", err)
		}
		out.Items = append(out.Items, it)
	}
	return &out, rows.Err()
}

func (s *Service) DraftFromLast(ctx context.Context, ownerID, modelID, personID, from string) (*Draft, error) {
	if modelID == "" || personID == "" {
		return nil, ErrInvalid
	}
	if from == "" {
		from = "last"
	}
	if from != "last" && from != "model" {
		return nil, ErrInvalid
	}
	studioID, err := s.ownerTime(ctx, ownerID)
	if err != nil {
		return nil, err
	}

	var n int
	if err := s.db.QueryRowContext(ctx, `
		SELECT count(*) FROM bonds
		WHERE person_id = $1 AND studio_id = $2 AND role = 'student' AND status = 'active'`,
		personID, studioID,
	).Scan(&n); err != nil {
		return nil, fmt.Errorf("publish draft bond: %w", err)
	}
	if n == 0 {
		return nil, ErrNotFound
	}

	var timeMatch int
	if err := s.db.QueryRowContext(ctx, `
		SELECT count(*) FROM models WHERE id = $1 AND studio_id = $2`,
		modelID, studioID,
	).Scan(&timeMatch); err != nil {
		return nil, fmt.Errorf("publish draft model: %w", err)
	}
	if timeMatch == 0 {
		return nil, ErrNotFound
	}

	tomorrow := s.now().AddDate(0, 0, 1).Format("2006-01-02")

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var draftID string
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO prescriptions (studio_id, person_id, model_id, for_date, status)
		VALUES ($1, $2, $3::uuid, $4::date, 'draft')
		RETURNING id::text`,
		studioID, personID, modelID, tomorrow,
	).Scan(&draftID); err != nil {
		return nil, fmt.Errorf("publish draft insert: %w", err)
	}

	if from == "model" {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO prescription_items (
				prescription_id, exercise_id, position, planned_sets, planned_reps,
				load_kg, rest_seconds, notes, load_source
			)
			SELECT $1, mi.exercise_id, mi.position, mi.planned_sets, mi.planned_reps,
			       COALESCE(mi.starter_load_kg, 0), mi.rest_seconds, mi.notes, 'starter'
			FROM model_items mi
			WHERE mi.model_id = $2::uuid`,
			draftID, modelID,
		); err != nil {
			return nil, fmt.Errorf("publish draft starter items: %w", err)
		}
	} else if _, err := tx.ExecContext(ctx, `
		INSERT INTO prescription_items (
			prescription_id, exercise_id, position, planned_sets, planned_reps,
			load_kg, rest_seconds, notes, load_source
		)
		SELECT $1, mi.exercise_id, mi.position, mi.planned_sets, mi.planned_reps,
		       COALESCE(hist.load_kg, last_set.load_kg, mi.starter_load_kg, 0),
		       mi.rest_seconds, mi.notes,
		       CASE
		           WHEN hist.load_kg IS NOT NULL OR last_set.load_kg IS NOT NULL THEN 'history'
		           ELSE 'starter'
		       END
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
		LEFT JOIN LATERAL (
			SELECT ws.load_kg
			FROM workout_sets ws
			JOIN workout_sessions sess ON sess.id = ws.session_id
			WHERE sess.person_id = $2 AND sess.studio_id = $3
			  AND ws.exercise_id = mi.exercise_id AND ws.load_kg IS NOT NULL
			ORDER BY ws.performed_at DESC
			LIMIT 1
		) last_set ON true
		WHERE mi.model_id = $5::uuid`,
		draftID, personID, studioID, tomorrow, modelID,
	); err != nil {
		return nil, fmt.Errorf("publish draft items: %w", err)
	}

	items, err := loadDraftItems(ctx, tx, draftID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &Draft{DraftID: draftID, Items: items}, nil
}

func loadDraftItems(ctx context.Context, q interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, draftID string) ([]DraftItem, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT pi.id::text, pi.exercise_id::text, e.name,
		       pi.planned_sets, pi.planned_reps, COALESCE(pi.load_kg, 0), pi.load_source
		FROM prescription_items pi
		JOIN exercises e ON e.id = pi.exercise_id
		WHERE pi.prescription_id = $1
		ORDER BY pi.position`,
		draftID,
	)
	if err != nil {
		return nil, fmt.Errorf("publish draft list: %w", err)
	}
	defer rows.Close()

	items := []DraftItem{}
	for rows.Next() {
		var it DraftItem
		if err := rows.Scan(&it.ID, &it.ExerciseID, &it.Name, &it.PlannedSets, &it.PlannedReps, &it.LoadKg, &it.LoadSource); err != nil {
			return nil, fmt.Errorf("publish draft list scan: %w", err)
		}
		items = append(items, it)
	}
	return items, rows.Err()
}

func (s *Service) PatchItem(ctx context.Context, ownerID, prescriptionID, itemID string, patch ItemPatch) error {
	if prescriptionID == "" || itemID == "" {
		return ErrNotFound
	}
	if patch.PlannedSets <= 0 || patch.PlannedReps == "" || patch.LoadKg < 0 {
		return ErrInvalid
	}
	studioID, err := s.ownerTime(ctx, ownerID)
	if err != nil {
		return err
	}

	var status string
	err = s.db.QueryRowContext(ctx, `
		SELECT status FROM prescriptions
		WHERE id = $1 AND studio_id = $2`,
		prescriptionID, studioID,
	).Scan(&status)
	if err == sql.ErrNoRows {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("publish patch status: %w", err)
	}
	if status != "draft" {
		return ErrInvalid
	}

	res, err := s.db.ExecContext(ctx, `
		UPDATE prescription_items
		SET load_kg = $3, planned_sets = $4, planned_reps = $5, load_source = 'manual'
		WHERE id = $1 AND prescription_id = $2`,
		itemID, prescriptionID, patch.LoadKg, patch.PlannedSets, patch.PlannedReps,
	)
	if err != nil {
		return fmt.Errorf("publish patch: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Service) Publish(ctx context.Context, ownerID, prescriptionID string, alsoPersonIDs []string) error {
	if prescriptionID == "" {
		return ErrInvalid
	}
	studioID, err := s.ownerTime(ctx, ownerID)
	if err != nil {
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var (
		personID string
		modelID  string
		forDate  string
		status   string
	)
	err = tx.QueryRowContext(ctx, `
		SELECT person_id::text, model_id::text, for_date::text, status
		FROM prescriptions
		WHERE id = $1 AND studio_id = $2
		FOR UPDATE`,
		prescriptionID, studioID,
	).Scan(&personID, &modelID, &forDate, &status)
	if err == sql.ErrNoRows {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("publish load: %w", err)
	}
	if status != "draft" {
		return ErrInvalid
	}
	if len(forDate) > 10 {
		forDate = forDate[:10]
	}

	now := s.now()
	if err := supersedePublished(ctx, tx, personID, studioID, forDate); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE prescriptions
		SET status = 'published', published_at = $2, updated_at = now()
		WHERE id = $1 AND status = 'draft'`,
		prescriptionID, now,
	); err != nil {
		return fmt.Errorf("publish draft: %w", err)
	}

	seen := map[string]bool{personID: true}
	for _, extraID := range alsoPersonIDs {
		if extraID == "" || seen[extraID] {
			continue
		}
		seen[extraID] = true
		if err := publishFromModelo(ctx, tx, studioID, extraID, modelID, forDate, now); err != nil {
			if err == ErrNotFound {
				continue
			}
			return err
		}
	}

	return tx.Commit()
}

func supersedePublished(ctx context.Context, tx *sql.Tx, personID, studioID, forDate string) error {
	if _, err := tx.ExecContext(ctx, `
		UPDATE prescriptions
		SET status = 'superseded', updated_at = now()
		WHERE person_id = $1 AND studio_id = $2 AND for_date = $3::date AND status = 'published'`,
		personID, studioID, forDate,
	); err != nil {
		return fmt.Errorf("publish supersede: %w", err)
	}
	return nil
}

func publishFromModelo(ctx context.Context, tx *sql.Tx, studioID, personID, modelID, forDate string, now time.Time) error {
	var n int
	if err := tx.QueryRowContext(ctx, `
		SELECT count(*) FROM bonds
		WHERE person_id = $1 AND studio_id = $2 AND role = 'student' AND status = 'active'`,
		personID, studioID,
	).Scan(&n); err != nil {
		return fmt.Errorf("publish extra bond: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}

	if err := supersedePublished(ctx, tx, personID, studioID, forDate); err != nil {
		return err
	}

	var prID string
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO prescriptions (studio_id, person_id, model_id, for_date, status, published_at)
		VALUES ($1, $2, $3::uuid, $4::date, 'published', $5)
		RETURNING id::text`,
		studioID, personID, modelID, forDate, now,
	).Scan(&prID); err != nil {
		return fmt.Errorf("publish extra: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO prescription_items (
			prescription_id, exercise_id, position, planned_sets, planned_reps,
			load_kg, rest_seconds, notes, load_source
		)
		SELECT $1, mi.exercise_id, mi.position, mi.planned_sets, mi.planned_reps,
		       COALESCE(last_set.load_kg, hist.load_kg, mi.starter_load_kg, 0),
		       mi.rest_seconds, mi.notes,
		       CASE
		           WHEN last_set.load_kg IS NOT NULL OR hist.load_kg IS NOT NULL THEN 'history'
		           ELSE 'starter'
		       END
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
		LEFT JOIN LATERAL (
			SELECT ws.load_kg
			FROM workout_sets ws
			JOIN workout_sessions sess ON sess.id = ws.session_id
			WHERE sess.person_id = $2 AND sess.studio_id = $3
			  AND ws.exercise_id = mi.exercise_id AND ws.load_kg IS NOT NULL
			ORDER BY ws.performed_at DESC
			LIMIT 1
		) last_set ON true
		WHERE mi.model_id = $5::uuid`,
		prID, personID, studioID, forDate, modelID,
	); err != nil {
		return fmt.Errorf("publish extra items: %w", err)
	}
	return nil
}
