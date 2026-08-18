package seed

import (
	"context"
	"database/sql"
	"fmt"
)

const (
	PhoneFred  = "+5511900000001"
	PhoneVitor = "+5511900000002"
	PhoneHuan  = "+5511900000003"
	PhoneJose  = "+5511900000004"

	StudioName   = "Estúdio Fred"
	AccentColor  = "#ec3013"
	InviteVitor  = "FRED-VITOR"
	InviteHuan   = "FRED-HUAN"
	InviteJose   = "FRED-JOSE"
)

type person struct {
	phone string
	name  string
	role  string
	code  string
}

func Dev(ctx context.Context, db *sql.DB) error {
	people := []person{
		{phone: PhoneFred, name: "Fred", role: "owner"},
		{phone: PhoneVitor, name: "Vitor", role: "student", code: InviteVitor},
		{phone: PhoneHuan, name: "Huan", role: "student", code: InviteHuan},
		{phone: PhoneJose, name: "Jose", role: "student", code: InviteJose},
	}

	for _, p := range people {
		if _, err := db.ExecContext(ctx, `
			INSERT INTO people (phone, name)
			VALUES ($1, $2)
			ON CONFLICT (phone) DO UPDATE SET name = EXCLUDED.name, updated_at = now()`,
			p.phone, p.name,
		); err != nil {
			return fmt.Errorf("person %s: %w", p.name, err)
		}
	}

	if _, err := db.ExecContext(ctx, `
		INSERT INTO studios (owner_person_id, name, accent_color)
		SELECT id, $1, $2 FROM people WHERE phone = $3
		ON CONFLICT (owner_person_id) DO UPDATE
		SET name = EXCLUDED.name, accent_color = EXCLUDED.accent_color, updated_at = now()`,
		StudioName, AccentColor, PhoneFred,
	); err != nil {
		return fmt.Errorf("studio: %w", err)
	}

	if _, err := db.ExecContext(ctx, `
		INSERT INTO bonds (person_id, studio_id, role, status)
		SELECT p.id, s.id, 'owner', 'active'
		FROM people p
		JOIN studios s ON s.owner_person_id = p.id
		WHERE p.phone = $1
		ON CONFLICT (person_id, studio_id) DO UPDATE
		SET role = 'owner', status = 'active', updated_at = now()`,
		PhoneFred,
	); err != nil {
		return fmt.Errorf("owner bond: %w", err)
	}

	for _, p := range people {
		if p.role != "student" {
			continue
		}
		if _, err := db.ExecContext(ctx, `
			INSERT INTO bonds (person_id, studio_id, role, status)
			SELECT p.id, s.id, 'student', 'active'
			FROM people p
			JOIN people owner ON owner.phone = $1
			JOIN studios s ON s.owner_person_id = owner.id
			WHERE p.phone = $2
			ON CONFLICT (person_id, studio_id) DO UPDATE
			SET role = 'student', status = 'active', updated_at = now()`,
			PhoneFred, p.phone,
		); err != nil {
			return fmt.Errorf("student bond %s: %w", p.name, err)
		}

		if _, err := db.ExecContext(ctx, `
			INSERT INTO invites (
				studio_id, created_by_person_id, phone, code,
				expires_at, accepted_at, accepted_person_id
			)
			SELECT s.id, owner.id, p.phone, $1, now() + interval '10 years', now(), p.id
			FROM people p
			JOIN people owner ON owner.phone = $2
			JOIN studios s ON s.owner_person_id = owner.id
			WHERE p.phone = $3
			ON CONFLICT (code) DO UPDATE
			SET accepted_at = COALESCE(invites.accepted_at, now()),
			    accepted_person_id = EXCLUDED.accepted_person_id`,
			p.code, PhoneFred, p.phone,
		); err != nil {
			return fmt.Errorf("invite %s: %w", p.name, err)
		}

		if _, err := db.ExecContext(ctx, `
			INSERT INTO streaks (bond_id, current_count, protector_available)
			SELECT b.id, 0, true
			FROM bonds b
			JOIN people p ON p.id = b.person_id
			JOIN studios s ON s.id = b.studio_id
			JOIN people owner ON owner.id = s.owner_person_id
			WHERE p.phone = $1 AND owner.phone = $2
			ON CONFLICT (bond_id) DO NOTHING`,
			p.phone, PhoneFred,
		); err != nil {
			return fmt.Errorf("streak %s: %w", p.name, err)
		}
	}

	if _, err := db.ExecContext(ctx, `
		UPDATE people p
		SET active_bond_id = b.id, updated_at = now()
		FROM bonds b
		JOIN studios s ON s.id = b.studio_id
		JOIN people owner ON owner.id = s.owner_person_id
		WHERE b.person_id = p.id
		  AND owner.phone = $1
		  AND p.phone IN ($1, $2, $3, $4)`,
		PhoneFred, PhoneVitor, PhoneHuan, PhoneJose,
	); err != nil {
		return fmt.Errorf("active bond: %w", err)
	}

	return nil
}
