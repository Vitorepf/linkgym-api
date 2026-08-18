package workout

import (
	"context"
	"database/sql"
	"encoding/json"
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

type Session struct {
	ID        string    `json:"id"`
	ClientID  string    `json:"client_id"`
	StartedAt time.Time `json:"started_at"`
}

type SetInput struct {
	ClientSetID           string
	PrescriptionItemID    string
	ExerciseID            string
	SwappedFromExerciseID *string
	SetIndex              int
	Reps                  int
	LoadKg                float64
	RestSeconds           int
	Effort                *int
	PerformedAt           time.Time
}

type Set struct {
	ID                    string    `json:"id"`
	ClientSetID           string    `json:"client_set_id"`
	PrescriptionItemID    *string   `json:"prescription_item_id"`
	ExerciseID            string    `json:"exercise_id"`
	SwappedFromExerciseID *string   `json:"swapped_from_exercise_id"`
	SetIndex              int       `json:"set_index"`
	Reps                  int       `json:"reps"`
	LoadKg                float64   `json:"load_kg"`
	RestSeconds           int       `json:"rest_seconds"`
	Effort                *int      `json:"effort"`
	PerformedAt           time.Time `json:"performed_at"`
}

type Streak struct {
	CurrentCount       int  `json:"current_count"`
	ProtectorAvailable bool `json:"protector_available"`
}

type Record struct {
	ExerciseName string  `json:"exercise_name"`
	LoadKg       float64 `json:"load_kg"`
	PreviousKg   float64 `json:"previous_kg"`
}

type FinishResult struct {
	Streak    Streak   `json:"streak"`
	XPGained  int      `json:"xp_gained"`
	XPTotal   int      `json:"xp_total"`
	Records   []Record `json:"records"`
	BadgeKeys []string `json:"badge_keys"`
}

func (s *Service) Start(ctx context.Context, personID, clientID, prescriptionID string) (*Session, error) {
	if personID == "" || clientID == "" || prescriptionID == "" {
		return nil, ErrInvalid
	}

	var studioID, ownerPerson string
	err := s.db.QueryRowContext(ctx, `
		SELECT pr.studio_id::text, pr.person_id::text
		FROM prescriptions pr
		WHERE pr.id = $1 AND pr.status = 'published'`,
		prescriptionID,
	).Scan(&studioID, &ownerPerson)
	if err == sql.ErrNoRows {
		return nil, ErrInvalid
	}
	if err != nil {
		return nil, fmt.Errorf("start prescription: %w", err)
	}
	if ownerPerson != personID {
		return nil, ErrForbidden
	}

	var out Session
	err = s.db.QueryRowContext(ctx, `
		INSERT INTO workout_sessions (person_id, studio_id, prescription_id, client_id, started_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (person_id, client_id) DO NOTHING
		RETURNING id::text, client_id::text, started_at`,
		personID, studioID, prescriptionID, clientID, s.now(),
	).Scan(&out.ID, &out.ClientID, &out.StartedAt)
	if err == sql.ErrNoRows {
		err = s.db.QueryRowContext(ctx, `
			SELECT id::text, client_id::text, started_at
			FROM workout_sessions
			WHERE person_id = $1 AND client_id = $2`,
			personID, clientID,
		).Scan(&out.ID, &out.ClientID, &out.StartedAt)
	}
	if err != nil {
		return nil, fmt.Errorf("start session: %w", err)
	}
	return &out, nil
}

func (s *Service) AddSet(ctx context.Context, personID, sessionID string, in SetInput) (*Set, error) {
	if err := s.ownSession(ctx, personID, sessionID); err != nil {
		return nil, err
	}
	if in.ClientSetID == "" || in.ExerciseID == "" || in.SetIndex < 1 {
		return nil, ErrInvalid
	}
	performed := in.PerformedAt
	if performed.IsZero() {
		performed = s.now()
	}

	var swapped any
	if in.SwappedFromExerciseID != nil && *in.SwappedFromExerciseID != "" {
		swapped = *in.SwappedFromExerciseID
	}
	var item any
	if in.PrescriptionItemID != "" {
		item = in.PrescriptionItemID
	}
	var effort any
	if in.Effort != nil {
		effort = *in.Effort
	}

	row := s.db.QueryRowContext(ctx, `
		INSERT INTO workout_sets (
			session_id, prescription_item_id, exercise_id, swapped_from_exercise_id,
			client_set_id, set_index, reps, load_kg, rest_seconds, effort, performed_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (session_id, client_set_id) DO NOTHING
		RETURNING id::text, client_set_id::text, prescription_item_id::text, exercise_id::text,
		          swapped_from_exercise_id::text, set_index, reps, load_kg, rest_seconds, effort, performed_at`,
		sessionID, item, in.ExerciseID, swapped, in.ClientSetID, in.SetIndex,
		in.Reps, in.LoadKg, in.RestSeconds, effort, performed,
	)
	got, err := scanSet(row)
	if err == sql.ErrNoRows {
		got, err = scanSet(s.db.QueryRowContext(ctx, `
			SELECT id::text, client_set_id::text, prescription_item_id::text, exercise_id::text,
			       swapped_from_exercise_id::text, set_index, reps, load_kg, rest_seconds, effort, performed_at
			FROM workout_sets
			WHERE session_id = $1 AND client_set_id = $2`,
			sessionID, in.ClientSetID,
		))
	}
	if err != nil {
		return nil, fmt.Errorf("add set: %w", err)
	}
	return got, nil
}

func (s *Service) Finish(ctx context.Context, personID, sessionID string, effort int) (*FinishResult, error) {
	if effort < 1 || effort > 3 {
		return nil, ErrInvalid
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var (
		ownerID, studioID, bondID string
		prescriptionID            sql.NullString
		finishedAt                sql.NullTime
	)
	err = tx.QueryRowContext(ctx, `
		SELECT ws.person_id::text, ws.studio_id::text, ws.prescription_id::text, ws.finished_at,
		       p.active_bond_id::text
		FROM workout_sessions ws
		JOIN people p ON p.id = ws.person_id
		WHERE ws.id = $1
		FOR UPDATE OF ws`,
		sessionID,
	).Scan(&ownerID, &studioID, &prescriptionID, &finishedAt, &bondID)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("finish lock: %w", err)
	}
	if ownerID != personID {
		return nil, ErrForbidden
	}

	if finishedAt.Valid {
		got, err := loadFinishSnapshot(ctx, tx, sessionID)
		if err != nil {
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return got, nil
	}

	now := s.now()
	if _, err := tx.ExecContext(ctx, `
		UPDATE workout_sessions
		SET finished_at = $2, synced_at = $2, effort = $3, updated_at = $2
		WHERE id = $1`,
		sessionID, now, effort,
	); err != nil {
		return nil, fmt.Errorf("finish stamp: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO xp_ledger (bond_id, amount, reason, session_id)
		VALUES ($1, 10, 'session', $2)`,
		bondID, sessionID,
	); err != nil {
		return nil, fmt.Errorf("finish xp session: %w", err)
	}

	records, prXP, err := s.upsertPRs(ctx, tx, personID, sessionID, now)
	if err != nil {
		return nil, err
	}
	for i := 0; i < prXP/25; i++ {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO xp_ledger (bond_id, amount, reason, session_id)
			VALUES ($1, 25, 'pr', $2)`,
			bondID, sessionID,
		); err != nil {
			return nil, fmt.Errorf("finish xp pr: %w", err)
		}
	}

	var forDate sql.NullTime
	if prescriptionID.Valid {
		if err := tx.QueryRowContext(ctx, `
			SELECT for_date FROM prescriptions WHERE id = $1`,
			prescriptionID.String,
		).Scan(&forDate); err != nil && err != sql.ErrNoRows {
			return nil, fmt.Errorf("finish for_date: %w", err)
		}
	}
	fulfilled := now.Format("2006-01-02")
	if forDate.Valid {
		fulfilled = forDate.Time.Format("2006-01-02")
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO streaks (bond_id, current_count, last_fulfilled_on, protector_available)
		VALUES ($1, 1, $2::date, true)
		ON CONFLICT (bond_id) DO UPDATE SET
			current_count = streaks.current_count + 1,
			last_fulfilled_on = EXCLUDED.last_fulfilled_on,
			updated_at = now()`,
		bondID, fulfilled,
	); err != nil {
		return nil, fmt.Errorf("finish streak: %w", err)
	}

	var streak Streak
	if err := tx.QueryRowContext(ctx, `
		SELECT current_count, protector_available FROM streaks WHERE bond_id = $1`,
		bondID,
	).Scan(&streak.CurrentCount, &streak.ProtectorAvailable); err != nil {
		return nil, fmt.Errorf("finish streak read: %w", err)
	}

	var xpTotal int
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(amount), 0) FROM xp_ledger WHERE bond_id = $1`,
		bondID,
	).Scan(&xpTotal); err != nil {
		return nil, fmt.Errorf("finish xp total: %w", err)
	}

	result := FinishResult{
		Streak:    streak,
		XPGained:  10 + prXP,
		XPTotal:   xpTotal,
		Records:   records,
		BadgeKeys: []string{},
	}
	if result.Records == nil {
		result.Records = []Record{}
	}

	payload, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO session_alerts (studio_id, person_id, session_id, kind, payload)
		VALUES ($1, $2, $3, 'session_synced', $4)`,
		studioID, personID, sessionID, payload,
	); err != nil {
		return nil, fmt.Errorf("finish alert: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &result, nil
}

type lift struct {
	exerciseID string
	name       string
	load       float64
	reps       sql.NullInt64
}

func (s *Service) upsertPRs(ctx context.Context, tx *sql.Tx, personID, sessionID string, now time.Time) ([]Record, int, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT DISTINCT ON (ws.exercise_id)
			ws.exercise_id::text, e.name, ws.load_kg, ws.reps
		FROM workout_sets ws
		JOIN exercises e ON e.id = ws.exercise_id
		WHERE ws.session_id = $1 AND ws.load_kg IS NOT NULL
		ORDER BY ws.exercise_id, ws.load_kg DESC, ws.performed_at DESC`,
		sessionID,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("finish sets: %w", err)
	}
	var lifts []lift
	for rows.Next() {
		var row lift
		if err := rows.Scan(&row.exerciseID, &row.name, &row.load, &row.reps); err != nil {
			_ = rows.Close()
			return nil, 0, err
		}
		lifts = append(lifts, row)
	}
	if err := rows.Close(); err != nil {
		return nil, 0, err
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	var (
		records []Record
		prXP    int
	)
	for _, row := range lifts {
		var previous sql.NullFloat64
		err := tx.QueryRowContext(ctx, `
			SELECT load_kg FROM personal_records
			WHERE person_id = $1 AND exercise_id = $2`,
			personID, row.exerciseID,
		).Scan(&previous)
		if err != nil && err != sql.ErrNoRows {
			return nil, 0, fmt.Errorf("finish pr read: %w", err)
		}
		isNew := err == sql.ErrNoRows
		if !isNew && previous.Float64 >= row.load {
			continue
		}
		prev := 0.0
		if !isNew {
			prev = previous.Float64
		}
		var repsVal any
		if row.reps.Valid {
			repsVal = int(row.reps.Int64)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO personal_records (person_id, exercise_id, load_kg, reps, session_id, achieved_at)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (person_id, exercise_id) DO UPDATE SET
				load_kg = EXCLUDED.load_kg,
				reps = EXCLUDED.reps,
				session_id = EXCLUDED.session_id,
				achieved_at = EXCLUDED.achieved_at`,
			personID, row.exerciseID, row.load, repsVal, sessionID, now,
		); err != nil {
			return nil, 0, fmt.Errorf("finish pr upsert: %w", err)
		}
		records = append(records, Record{
			ExerciseName: row.name,
			LoadKg:       row.load,
			PreviousKg:   prev,
		})
		prXP += 25
	}
	return records, prXP, nil
}

func loadFinishSnapshot(ctx context.Context, tx *sql.Tx, sessionID string) (*FinishResult, error) {
	var raw []byte
	err := tx.QueryRowContext(ctx, `
		SELECT payload FROM session_alerts
		WHERE session_id = $1 AND kind = 'session_synced'
		ORDER BY created_at
		LIMIT 1`,
		sessionID,
	).Scan(&raw)
	if err == sql.ErrNoRows {
		return nil, ErrInvalid
	}
	if err != nil {
		return nil, fmt.Errorf("finish snapshot: %w", err)
	}
	var got FinishResult
	if err := json.Unmarshal(raw, &got); err != nil {
		return nil, err
	}
	if got.Records == nil {
		got.Records = []Record{}
	}
	if got.BadgeKeys == nil {
		got.BadgeKeys = []string{}
	}
	return &got, nil
}

func (s *Service) ownSession(ctx context.Context, personID, sessionID string) error {
	var owner string
	err := s.db.QueryRowContext(ctx, `
		SELECT person_id::text FROM workout_sessions WHERE id = $1`,
		sessionID,
	).Scan(&owner)
	if err == sql.ErrNoRows {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if owner != personID {
		return ErrForbidden
	}
	return nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanSet(row scanner) (*Set, error) {
	var (
		out     Set
		item    sql.NullString
		swapped sql.NullString
		reps    sql.NullInt64
		load    sql.NullFloat64
		rest    sql.NullInt64
		effort  sql.NullFloat64
	)
	err := row.Scan(
		&out.ID, &out.ClientSetID, &item, &out.ExerciseID, &swapped,
		&out.SetIndex, &reps, &load, &rest, &effort, &out.PerformedAt,
	)
	if err != nil {
		return nil, err
	}
	if item.Valid {
		v := item.String
		out.PrescriptionItemID = &v
	}
	if swapped.Valid {
		v := swapped.String
		out.SwappedFromExerciseID = &v
	}
	if reps.Valid {
		out.Reps = int(reps.Int64)
	}
	if load.Valid {
		out.LoadKg = load.Float64
	}
	if rest.Valid {
		out.RestSeconds = int(rest.Int64)
	}
	if effort.Valid {
		v := int(effort.Float64)
		out.Effort = &v
	}
	return &out, nil
}
