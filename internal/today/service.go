package today

import (
	"context"
	"database/sql"
	"fmt"
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
	day := s.now().Format("2006-01-02")

	var (
		out    Payload
		bondID string
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT p.id, p.name,
		       s.id, s.name, s.accent_color,
		       b.id,
		       COALESCE(st.current_count, 0),
		       COALESCE(st.protector_available, true)
		FROM people p
		JOIN bonds b ON b.id = p.active_bond_id
		JOIN studios s ON s.id = b.studio_id
		LEFT JOIN streaks st ON st.bond_id = b.id
		WHERE p.id = $1`,
		personID,
	).Scan(
		&out.Person.ID, &out.Person.Name,
		&out.Studio.ID, &out.Studio.Name, &out.Studio.AccentColor,
		&bondID,
		&out.Streak.CurrentCount, &out.Streak.ProtectorAvailable,
	)
	if err != nil {
		return nil, fmt.Errorf("today person: %w", err)
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
	if err == nil {
		out.Readiness.Energy = int(energy.Int64)
		out.Readiness.Soreness = int(soreness.Int64)
		out.Readiness.Sleep = int(sleep.Int64)
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

func formatKg(kg float64) string {
	if kg == float64(int64(kg)) {
		return strconv.FormatInt(int64(kg), 10)
	}
	return strconv.FormatFloat(kg, 'f', -1, 64)
}
