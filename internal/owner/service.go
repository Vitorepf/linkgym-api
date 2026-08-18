package owner

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

type Home struct {
	Greeting      string      `json:"greeting"`
	StudentCount  int         `json:"student_count"`
	Fio           Fio         `json:"fio"`
	Attention     []Attention `json:"attention"`
	UnreadReturns int         `json:"unread_returns"`
}

type Fio struct {
	Prescribed int      `json:"prescribed"`
	Done       int      `json:"done"`
	Week       []FioDay `json:"week"`
}

type FioDay struct {
	ForDate    string `json:"for_date"`
	Done       int    `json:"done"`
	Prescribed int    `json:"prescribed"`
}

type Attention struct {
	ID       string `json:"id"`
	PersonID string `json:"person_id"`
	Name     string `json:"name"`
	Reason   string `json:"reason"`
	Decision string `json:"decision"`
	Rank     int    `json:"rank"`
	Applied  bool   `json:"applied"`
	Days     int    `json:"days,omitempty"`
}

func (s *Service) Home(ctx context.Context, personID string) (*Home, error) {
	day := s.now().Format("2006-01-02")

	var name, studioID string
	err := s.db.QueryRowContext(ctx, `
		SELECT p.name, b.studio_id::text
		FROM people p
		JOIN bonds b ON b.id = p.active_bond_id
		WHERE p.id = $1`,
		personID,
	).Scan(&name, &studioID)
	if err != nil {
		return nil, fmt.Errorf("owner home person: %w", err)
	}

	out := Home{
		Greeting:      fmt.Sprintf("Bom dia, %s", name),
		UnreadReturns: 0,
		Attention:     []Attention{},
	}

	if err := s.db.QueryRowContext(ctx, `
		SELECT count(*) FROM bonds
		WHERE studio_id = $1 AND role = 'student' AND status = 'active'`,
		studioID,
	).Scan(&out.StudentCount); err != nil {
		return nil, fmt.Errorf("owner home students: %w", err)
	}

	attention, err := s.loadAttention(ctx, studioID, day)
	if err != nil {
		return nil, err
	}
	out.Attention = attention

	fio, err := s.loadFio(ctx, studioID, day)
	if err != nil {
		return nil, err
	}
	out.Fio = fio

	if err := s.db.QueryRowContext(ctx, `
		SELECT count(*) FROM session_alerts
		WHERE studio_id = $1 AND kind = 'session_synced' AND read_at IS NULL`,
		studioID,
	).Scan(&out.UnreadReturns); err != nil {
		return nil, fmt.Errorf("owner home returns: %w", err)
	}
	return &out, nil
}

type Record struct {
	ExerciseName string  `json:"exercise_name"`
	LoadKg       float64 `json:"load_kg"`
	PreviousKg   float64 `json:"previous_kg"`
}

type ReturnItem struct {
	AlertID   string    `json:"alert_id"`
	PersonID  string    `json:"person_id"`
	Name      string    `json:"name"`
	Effort    int       `json:"effort"`
	Records   []Record  `json:"records"`
	CreatedAt time.Time `json:"created_at"`
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

func (s *Service) Returns(ctx context.Context, ownerID string) ([]ReturnItem, error) {
	studioID, err := s.ownerTime(ctx, ownerID)
	if err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT a.id::text, a.person_id::text, p.name, COALESCE(ws.effort, 0), a.payload, a.created_at
		FROM session_alerts a
		JOIN people p ON p.id = a.person_id
		LEFT JOIN workout_sessions ws ON ws.id = a.session_id
		WHERE a.studio_id = $1 AND a.kind = 'session_synced' AND a.read_at IS NULL
		ORDER BY a.created_at DESC`,
		studioID,
	)
	if err != nil {
		return nil, fmt.Errorf("owner returns: %w", err)
	}
	defer rows.Close()

	items := []ReturnItem{}
	for rows.Next() {
		var (
			it     ReturnItem
			effort float64
			raw    []byte
		)
		if err := rows.Scan(&it.AlertID, &it.PersonID, &it.Name, &effort, &raw, &it.CreatedAt); err != nil {
			return nil, fmt.Errorf("owner returns scan: %w", err)
		}
		it.Effort = int(effort)
		it.Records = []Record{}
		var payload struct {
			Records []Record `json:"records"`
		}
		if len(raw) > 0 && json.Unmarshal(raw, &payload) == nil && payload.Records != nil {
			it.Records = payload.Records
		}
		items = append(items, it)
	}
	return items, rows.Err()
}

func allowedBump(v float64) bool {
	return v == 2.5 || v == 0 || v == -2.5
}

func (s *Service) ApplyReturn(ctx context.Context, ownerID, alertID string, bump float64) error {
	if alertID == "" || !allowedBump(bump) {
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
		personID, sessionID sql.NullString
		kind                string
		readAt              sql.NullTime
	)
	err = tx.QueryRowContext(ctx, `
		SELECT person_id::text, session_id::text, kind, read_at
		FROM session_alerts
		WHERE id = $1 AND studio_id = $2
		FOR UPDATE`,
		alertID, studioID,
	).Scan(&personID, &sessionID, &kind, &readAt)
	if err == sql.ErrNoRows {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("owner apply alert: %w", err)
	}
	if kind != "session_synced" || readAt.Valid || !personID.Valid {
		return ErrNotFound
	}

	now := s.now()
	if _, err := tx.ExecContext(ctx, `
		UPDATE session_alerts SET read_at = $2 WHERE id = $1`,
		alertID, now,
	); err != nil {
		return fmt.Errorf("owner apply read: %w", err)
	}

	tomorrow := now.AddDate(0, 0, 1).Format("2006-01-02")
	var draftID string
	err = tx.QueryRowContext(ctx, `
		SELECT id::text FROM prescriptions
		WHERE person_id = $1 AND studio_id = $2 AND for_date = $3 AND status = 'draft'
		ORDER BY created_at DESC
		LIMIT 1
		FOR UPDATE`,
		personID.String, studioID, tomorrow,
	).Scan(&draftID)
	if err == sql.ErrNoRows {
		if err := s.insertTomorrowDraft(ctx, tx, sessionID, tomorrow, bump, &draftID); err != nil {
			return err
		}
	} else if err != nil {
		return fmt.Errorf("owner apply draft: %w", err)
	} else if _, err := tx.ExecContext(ctx, `
		UPDATE prescription_items
		SET load_kg = GREATEST(0, COALESCE(load_kg, 0) + $2), load_source = 'manual'
		WHERE prescription_id = $1
		  AND position = (SELECT MIN(position) FROM prescription_items WHERE prescription_id = $1)`,
		draftID, bump,
	); err != nil {
		return fmt.Errorf("owner apply bump: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	return nil
}

func (s *Service) insertTomorrowDraft(ctx context.Context, tx *sql.Tx, sessionID sql.NullString, tomorrow string, bump float64, draftID *string) error {
	if !sessionID.Valid {
		return ErrInvalid
	}
	var sourceID sql.NullString
	err := tx.QueryRowContext(ctx, `
		SELECT prescription_id::text FROM workout_sessions WHERE id = $1`,
		sessionID.String,
	).Scan(&sourceID)
	if err == sql.ErrNoRows || !sourceID.Valid {
		return ErrInvalid
	}
	if err != nil {
		return fmt.Errorf("owner apply session: %w", err)
	}
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO prescriptions (studio_id, person_id, model_id, for_date, status)
		SELECT studio_id, person_id, model_id, $2::date, 'draft'
		FROM prescriptions WHERE id = $1
		RETURNING id::text`,
		sourceID.String, tomorrow,
	).Scan(draftID); err != nil {
		return fmt.Errorf("owner apply insert: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO prescription_items (
			prescription_id, exercise_id, position, planned_sets, planned_reps,
			load_kg, rest_seconds, notes, load_source
		)
		SELECT $1, exercise_id, position, planned_sets, planned_reps,
			GREATEST(0, COALESCE(load_kg, 0) + CASE WHEN position = (
				SELECT MIN(position) FROM prescription_items WHERE prescription_id = $2
			) THEN $3::numeric ELSE 0 END),
			rest_seconds, notes, 'manual'
		FROM prescription_items
		WHERE prescription_id = $2`,
		*draftID, sourceID.String, bump,
	); err != nil {
		return fmt.Errorf("owner apply items: %w", err)
	}
	return nil
}

func (s *Service) loadAttention(ctx context.Context, studioID, day string) ([]Attention, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT a.id::text, a.person_id::text, p.name, a.reason, a.rank,
		       GREATEST(1, COALESCE(
		           $2::date - st.last_fulfilled_on,
		           $2::date - (
		               SELECT MAX(ws.finished_at::date)
		               FROM workout_sessions ws
		               WHERE ws.person_id = a.person_id
		                 AND ws.studio_id = a.studio_id
		                 AND ws.finished_at IS NOT NULL
		           ),
		           1
		       ))
		FROM attention_items a
		JOIN people p ON p.id = a.person_id
		LEFT JOIN bonds b ON b.person_id = a.person_id AND b.studio_id = a.studio_id AND b.status = 'active'
		LEFT JOIN streaks st ON st.bond_id = b.id
		WHERE a.studio_id = $1 AND a.for_date = $2
		ORDER BY a.rank
		LIMIT 3`,
		studioID, day,
	)
	if err != nil {
		return nil, fmt.Errorf("owner attention: %w", err)
	}
	defer rows.Close()

	items := []Attention{}
	for rows.Next() {
		var it Attention
		if err := rows.Scan(&it.ID, &it.PersonID, &it.Name, &it.Reason, &it.Rank, &it.Days); err != nil {
			return nil, fmt.Errorf("owner attention scan: %w", err)
		}
		it.Decision = decisionFor(it.Reason)
		items = append(items, it)
	}
	return items, rows.Err()
}

func (s *Service) loadFio(ctx context.Context, studioID, today string) (Fio, error) {
	now := s.now()
	mon, sun := isoWeekBounds(now)

	rows, err := s.db.QueryContext(ctx, `
		SELECT for_date::text,
		       count(*) AS prescribed,
		       count(*) FILTER (WHERE status = 'done') AS done
		FROM fio_days
		WHERE studio_id = $1 AND for_date BETWEEN $2 AND $3
		GROUP BY for_date`,
		studioID, mon.Format("2006-01-02"), sun.Format("2006-01-02"),
	)
	if err != nil {
		return Fio{}, fmt.Errorf("owner fio: %w", err)
	}
	defer rows.Close()

	byDay := map[string]FioDay{}
	for rows.Next() {
		var d FioDay
		if err := rows.Scan(&d.ForDate, &d.Prescribed, &d.Done); err != nil {
			return Fio{}, fmt.Errorf("owner fio scan: %w", err)
		}
		d.ForDate = dateKey(d.ForDate)
		byDay[d.ForDate] = d
	}
	if err := rows.Err(); err != nil {
		return Fio{}, err
	}

	week := make([]FioDay, 0, 7)
	for d := mon; !d.After(sun); d = d.AddDate(0, 0, 1) {
		key := d.Format("2006-01-02")
		if hit, ok := byDay[key]; ok {
			week = append(week, hit)
			continue
		}
		week = append(week, FioDay{ForDate: key})
	}

	todayFio := byDay[dateKey(today)]
	return Fio{
		Prescribed: todayFio.Prescribed,
		Done:       todayFio.Done,
		Week:       week,
	}, nil
}

// dateKey keeps map keys as YYYY-MM-DD so SQL DATE::text and Go today match.
func dateKey(s string) string {
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t.Format("2006-01-02")
	}
	if len(s) >= 10 {
		if t, err := time.Parse("2006-01-02", s[:10]); err == nil {
			return t.Format("2006-01-02")
		}
	}
	return s
}

func isoWeekBounds(t time.Time) (time.Time, time.Time) {
	t = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	wd := int(t.Weekday())
	if wd == 0 {
		wd = 7
	}
	mon := t.AddDate(0, 0, 1-wd)
	return mon, mon.AddDate(0, 0, 6)
}

func decisionFor(reason string) string {
	switch reason {
	case "student_stopped":
		return "Mandar a retomada de 9 minutos"
	case "pain_flag":
		return "Ligar antes de publicar"
	case "debut":
		return "Publicar o treino curto"
	case "high_effort":
		return "Manter carga"
	default:
		return ""
	}
}
