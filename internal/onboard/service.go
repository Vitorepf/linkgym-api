package onboard

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

var ErrInvalid = errors.New("invalido")

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

type Answers struct {
	Experience  string `json:"experience"`
	DaysPerWeek int    `json:"days_per_week"`
	Pain        bool   `json:"pain"`
}

func (s *Service) Put(ctx context.Context, personID string, in Answers) error {
	switch in.Experience {
	case "never", "before", "training":
	default:
		return ErrInvalid
	}
	if in.DaysPerWeek < 2 || in.DaysPerWeek > 6 {
		return ErrInvalid
	}

	payload, err := json.Marshal(in)
	if err != nil {
		return fmt.Errorf("onboard marshal: %w", err)
	}

	var studioID string
	err = s.db.QueryRowContext(ctx, `
		UPDATE bonds b
		SET onboarding = $2::jsonb, updated_at = now()
		FROM people p
		WHERE p.id = $1 AND b.id = p.active_bond_id AND b.role = 'student'
		RETURNING b.studio_id::text`,
		personID, payload,
	).Scan(&studioID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrInvalid
	}
	if err != nil {
		return fmt.Errorf("onboard write: %w", err)
	}

	if !in.Pain {
		return nil
	}

	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO attention_items (studio_id, person_id, for_date, reason, rank)
		VALUES ($1, $2, $3::date, 'pain_flag', 1)
		ON CONFLICT (studio_id, person_id, for_date) DO UPDATE
		SET reason = 'pain_flag', rank = 1`,
		studioID, personID, s.now().Format("2006-01-02"),
	); err != nil {
		return fmt.Errorf("onboard attention: %w", err)
	}
	return nil
}

type Commitment struct {
	DaysPerWeek int `json:"days_per_week"`
}

func (s *Service) Commit(ctx context.Context, personID string, days int) error {
	if days < 2 || days > 6 {
		return ErrInvalid
	}

	res, err := s.db.ExecContext(ctx, `
		UPDATE bonds b
		SET commitment_text = $2, commitment_at = $3, updated_at = now()
		FROM people p
		WHERE p.id = $1 AND b.id = p.active_bond_id AND b.role = 'student'`,
		personID, fmt.Sprintf("%d dias", days), s.now(),
	)
	if err != nil {
		return fmt.Errorf("commitment write: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("commitment rows: %w", err)
	}
	if n == 0 {
		return ErrInvalid
	}
	return nil
}
