package owner

import (
	"context"
	"database/sql"
	"fmt"
)

func (s *Service) Attention(ctx context.Context, ownerID string) ([]Attention, error) {
	studioID, err := s.ownerTime(ctx, ownerID)
	if err != nil {
		return nil, err
	}
	day := s.now().Format("2006-01-02")
	return s.loadAttention(ctx, studioID, day)
}

func (s *Service) Apply(ctx context.Context, ownerID, attentionID string) error {
	if attentionID == "" {
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

	var personID, reason string
	err = tx.QueryRowContext(ctx, `
		SELECT person_id::text, reason
		FROM attention_items
		WHERE id = $1 AND studio_id = $2 AND applied_at IS NULL
		FOR UPDATE`,
		attentionID, studioID,
	).Scan(&personID, &reason)
	if err == sql.ErrNoRows {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("owner attention apply: %w", err)
	}

	// APPLIED_AT, e não DELETE. A linha anterior apagava o item: toda vez que o personal
	// agia sobre a fila, o sistema destruía o registro de que tinha sinalizado aquela pessoa
	// E de que ele tinha agido. O campo `Applied bool` do struct já existia, já era
	// serializado, e nunca foi preenchido por ninguém — sem esta memória não há como
	// perguntar se a fila acerta, e a resposta só existe olhando para trás.
	if _, err := tx.ExecContext(ctx,
		`UPDATE attention_items SET applied_at = now() WHERE id = $1 AND applied_at IS NULL`,
		attentionID,
	); err != nil {
		return fmt.Errorf("owner attention aplicar: %w", err)
	}

	if reason == "student_stopped" {
		if err := upsertOpenComeback(ctx, tx, personID, studioID, s.now().Format("2006-01-02")); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func upsertOpenComeback(ctx context.Context, tx *sql.Tx, personID, studioID, missedOn string) error {
	var bondID string
	err := tx.QueryRowContext(ctx, `
		SELECT id::text FROM bonds
		WHERE person_id = $1 AND studio_id = $2 AND status = 'active'`,
		personID, studioID,
	).Scan(&bondID)
	if err == sql.ErrNoRows {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("owner attention bond: %w", err)
	}

	var openID sql.NullString
	if err := tx.QueryRowContext(ctx, `
		SELECT id::text FROM comebacks
		WHERE bond_id = $1 AND completed_at IS NULL
		LIMIT 1`,
		bondID,
	).Scan(&openID); err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("owner attention comeback: %w", err)
	}
	if openID.Valid {
		return nil
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO comebacks (bond_id, missed_on)
		VALUES ($1, $2::date)
		ON CONFLICT (bond_id, missed_on) DO NOTHING`,
		bondID, missedOn,
	); err != nil {
		return fmt.Errorf("owner attention comeback insert: %w", err)
	}
	return nil
}
