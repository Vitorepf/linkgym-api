package progress

import (
	"context"
	"database/sql"
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
	Ofensiva      Ofensiva       `json:"ofensiva"`
	XPTotal       int            `json:"xp_total"`
	League        []LeagueRow    `json:"league"`
	Badges        []Badge        `json:"badges"`
	ProntidaoWeek []ProntidaoDay `json:"prontidao_week"`
}

type Ofensiva struct {
	CurrentCount       int  `json:"current_count"`
	ProtectorAvailable bool `json:"protector_available"`
}

type LeagueRow struct {
	Name    string `json:"name"`
	XPTotal int    `json:"xp_total"`
	Me      bool   `json:"me"`
}

type Badge struct {
	BadgeKey string    `json:"badge_key"`
	EarnedAt time.Time `json:"earned_at"`
}

type ProntidaoDay struct {
	ForDate string `json:"for_date"`
	Score   int    `json:"score"`
}

type Records struct {
	Items []RecordItem `json:"items"`
}

type RecordItem struct {
	ExerciseName string          `json:"exercise_name"`
	LoadKg       float64         `json:"load_kg"`
	Reps         int             `json:"reps"`
	AchievedAt   time.Time       `json:"achieved_at"`
	History      []RecordHistory `json:"history"`
}

type RecordHistory struct {
	LoadKg     float64   `json:"load_kg"`
	AchievedAt time.Time `json:"achieved_at"`
}

func (s *Service) Get(ctx context.Context, personID string) (*Payload, error) {
	var (
		out      Payload
		bondID   string
		studioID string
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT b.id::text, b.studio_id::text,
		       COALESCE(st.current_count, 0),
		       COALESCE(st.protector_available, true)
		FROM people p
		JOIN bonds b ON b.id = p.active_bond_id
		LEFT JOIN streaks st ON st.bond_id = b.id
		WHERE p.id = $1`,
		personID,
	).Scan(&bondID, &studioID, &out.Ofensiva.CurrentCount, &out.Ofensiva.ProtectorAvailable)
	if err != nil {
		return nil, fmt.Errorf("progress person: %w", err)
	}

	if err := s.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(amount), 0) FROM xp_ledger WHERE bond_id = $1`,
		bondID,
	).Scan(&out.XPTotal); err != nil {
		return nil, fmt.Errorf("progress xp: %w", err)
	}

	league, err := s.league(ctx, studioID, personID)
	if err != nil {
		return nil, err
	}
	out.League = league

	badges, err := s.badges(ctx, studioID, personID)
	if err != nil {
		return nil, err
	}
	out.Badges = badges

	week, err := s.prontidaoWeek(ctx, personID)
	if err != nil {
		return nil, err
	}
	out.ProntidaoWeek = week
	return &out, nil
}

func (s *Service) league(ctx context.Context, studioID, personID string) ([]LeagueRow, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.name, COALESCE(SUM(x.amount), 0)::int, p.id = $2
		FROM bonds b
		JOIN people p ON p.id = b.person_id
		LEFT JOIN xp_ledger x ON x.bond_id = b.id
		WHERE b.studio_id = $1 AND b.role = 'student' AND b.status = 'active'
		GROUP BY p.id, p.name
		ORDER BY COALESCE(SUM(x.amount), 0) DESC, p.name ASC
		LIMIT 10`,
		studioID, personID,
	)
	if err != nil {
		return nil, fmt.Errorf("progress league: %w", err)
	}
	defer rows.Close()

	out := []LeagueRow{}
	for rows.Next() {
		var row LeagueRow
		if err := rows.Scan(&row.Name, &row.XPTotal, &row.Me); err != nil {
			return nil, fmt.Errorf("progress league scan: %w", err)
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s *Service) badges(ctx context.Context, studioID, personID string) ([]Badge, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT badge_key, earned_at
		FROM badges
		WHERE studio_id = $1 AND person_id = $2
		ORDER BY earned_at`,
		studioID, personID,
	)
	if err != nil {
		return nil, fmt.Errorf("progress badges: %w", err)
	}
	defer rows.Close()

	out := []Badge{}
	for rows.Next() {
		var row Badge
		if err := rows.Scan(&row.BadgeKey, &row.EarnedAt); err != nil {
			return nil, fmt.Errorf("progress badge scan: %w", err)
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s *Service) prontidaoWeek(ctx context.Context, personID string) ([]ProntidaoDay, error) {
	day := s.now().Format("2006-01-02")
	rows, err := s.db.QueryContext(ctx, `
		SELECT d::date::text, r.energy, r.soreness, r.sleep
		FROM generate_series($2::date - 6, $2::date, interval '1 day') AS d
		LEFT JOIN readiness_logs r ON r.person_id = $1 AND r.for_date = d::date
		ORDER BY d`,
		personID, day,
	)
	if err != nil {
		return nil, fmt.Errorf("progress prontidao: %w", err)
	}
	defer rows.Close()

	out := []ProntidaoDay{}
	for rows.Next() {
		var (
			row                     ProntidaoDay
			energy, soreness, sleep sql.NullInt64
		)
		if err := rows.Scan(&row.ForDate, &energy, &soreness, &sleep); err != nil {
			return nil, fmt.Errorf("progress prontidao scan: %w", err)
		}
		if energy.Valid && soreness.Valid && sleep.Valid {
			row.Score = int(math.Round(float64(int(energy.Int64)+(6-int(soreness.Int64))+int(sleep.Int64)) / 15.0 * 100))
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s *Service) Records(ctx context.Context, personID string) (*Records, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT e.name, pr.load_kg, pr.reps, pr.achieved_at
		FROM personal_records pr
		JOIN exercises e ON e.id = pr.exercise_id
		WHERE pr.person_id = $1
		ORDER BY pr.achieved_at DESC`,
		personID,
	)
	if err != nil {
		return nil, fmt.Errorf("records: %w", err)
	}
	defer rows.Close()

	out := Records{Items: []RecordItem{}}
	for rows.Next() {
		var (
			item RecordItem
			reps sql.NullInt64
		)
		if err := rows.Scan(&item.ExerciseName, &item.LoadKg, &reps, &item.AchievedAt); err != nil {
			return nil, fmt.Errorf("records scan: %w", err)
		}
		if reps.Valid {
			item.Reps = int(reps.Int64)
		}
		item.History = []RecordHistory{{
			LoadKg:     item.LoadKg,
			AchievedAt: item.AchievedAt,
		}}
		out.Items = append(out.Items, item)
	}
	return &out, rows.Err()
}
