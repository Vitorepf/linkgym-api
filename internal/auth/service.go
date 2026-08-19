package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"
)

type Service struct {
	db     *sql.DB
	pepper string
	dev    bool
	now    func() time.Time
}

func New(db *sql.DB, pepper string, dev bool) *Service {
	if pepper == "" {
		pepper = "linkgym-dev-pepper"
	}
	return &Service{db: db, pepper: pepper, dev: dev, now: time.Now}
}

type Session struct {
	Token              string
	Person             Person
	Time               Time
	OnboardingComplete bool
	CommitmentComplete bool
	Debut              bool
}

type Person struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Phone string `json:"phone"`
	Role  string `json:"role"`
	// A chave fica fora do JSON: quem vai para o app é a URL presignada, que o
	// cmd/api monta com o Signer. Cor vai crua — é só um hex escolhido.
	AvatarKey   string `json:"-"`
	AvatarColor string `json:"avatar_color,omitempty"`
	AvatarURL   string `json:"avatar_url,omitempty"`
}

type Time struct {
	ID      string          `json:"id"`
	Name    string          `json:"name"`
	Accent  string          `json:"accent_color"`
	LogoKey string          `json:"-"`
	LogoURL string          `json:"logo_url,omitempty"`
	Config  json.RawMessage `json:"config"`
}

func (s *Service) RequestCode(ctx context.Context, rawPhone, invite string) (devCode string, tm *Time, err error) {
	phone, err := NormalizePhone(rawPhone)
	if err != nil {
		return "", nil, err
	}
	invite = strings.ToUpper(strings.TrimSpace(invite))
	invite, err = s.resolverConvite(ctx, phone, invite)
	if err != nil {
		return "", nil, err
	}

	var personID string
	err = s.db.QueryRowContext(ctx, `SELECT id FROM people WHERE phone = $1`, phone).Scan(&personID)
	if errors.Is(err, sql.ErrNoRows) {
		if invite == "" {
			return "", nil, ErrInviteRequired
		}
		var inviteID string
		err = s.db.QueryRowContext(ctx, `
			SELECT id FROM invites
			WHERE code = $1 AND phone = $2 AND expires_at > $3`,
			invite, phone, s.now(),
		).Scan(&inviteID)
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil, ErrInviteInvalid
		}
		if err != nil {
			return "", nil, fmtErr("invite", err)
		}
	} else if err != nil {
		return "", nil, fmtErr("person", err)
	}

	if invite != "" {
		var st Time
		// O logo e a config vêm JUNTO. A tela de convite é a primeira coisa que o aluno vê
		// do estúdio, e sem estas duas colunas ela era a única tela do produto que todo
		// aluno de todo personal via igual: sem rosto e no chão de fábrica.
		err = s.db.QueryRowContext(ctx, `
			SELECT s.id::text, s.name, s.accent_color,
			       COALESCE(s.logo_object_key, ''), COALESCE(s.config, '{}'::jsonb)
			FROM invites i
			JOIN studios s ON s.id = i.studio_id
			WHERE i.code = $1 AND i.expires_at > $2`,
			invite, s.now(),
		).Scan(&st.ID, &st.Name, &st.Accent, &st.LogoKey, &st.Config)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return "", nil, fmtErr("invite time", err)
		}
		if err == nil {
			tm = &st
		}
	}

	code := DevCode
	if !s.dev {
		code, err = randomDigits(4)
		if err != nil {
			return "", nil, err
		}
	}

	if _, err := s.db.ExecContext(ctx, `
		UPDATE login_codes SET consumed_at = $2
		WHERE phone = $1 AND consumed_at IS NULL`,
		phone, s.now(),
	); err != nil {
		return "", nil, fmtErr("invalidate", err)
	}

	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO login_codes (phone, code_hash, expires_at)
		VALUES ($1, $2, $3)`,
		phone, hashBytes(s.pepper, phone, code), s.now().Add(5*time.Minute),
	); err != nil {
		return "", nil, fmtErr("insert code", err)
	}

	if s.dev {
		return code, tm, nil
	}
	return "", tm, nil
}

func (s *Service) Verify(ctx context.Context, rawPhone, code, invite string) (*Session, error) {
	phone, err := NormalizePhone(rawPhone)
	if err != nil {
		return nil, err
	}
	code = strings.TrimSpace(code)
	if len(code) != 4 {
		return nil, ErrCodeInvalid
	}
	invite = strings.ToUpper(strings.TrimSpace(invite))
	invite, err = s.resolverConvite(ctx, phone, invite)
	if err != nil {
		return nil, err
	}

	var codeID string
	var hash []byte
	var attempts int
	err = s.db.QueryRowContext(ctx, `
		SELECT id, code_hash, attempt_count FROM login_codes
		WHERE phone = $1 AND consumed_at IS NULL AND expires_at > $2
		ORDER BY created_at DESC
		LIMIT 1`,
		phone, s.now(),
	).Scan(&codeID, &hash, &attempts)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCodeInvalid
	}
	if err != nil {
		return nil, fmtErr("lookup code", err)
	}

	want := hashBytes(s.pepper, phone, code)
	if !bytesEqual(hash, want) {
		if _, err := s.db.ExecContext(ctx, `
			UPDATE login_codes SET attempt_count = attempt_count + 1,
			consumed_at = CASE WHEN attempt_count >= 4 THEN $2 ELSE consumed_at END
			WHERE id = $1`,
			codeID, s.now(),
		); err != nil {
			return nil, fmtErr("attempt", err)
		}
		return nil, ErrCodeInvalid
	}

	if _, err := s.db.ExecContext(ctx, `
		UPDATE login_codes SET consumed_at = $2 WHERE id = $1`,
		codeID, s.now(),
	); err != nil {
		return nil, fmtErr("consume", err)
	}

	personID, err := s.ensurePerson(ctx, phone, invite)
	if err != nil {
		return nil, err
	}

	raw, tokHash, err := randomToken()
	if err != nil {
		return nil, err
	}
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO auth_sessions (person_id, token_hash, expires_at)
		VALUES ($1, $2, $3)`,
		personID, tokHash, s.now().Add(30*24*time.Hour),
	); err != nil {
		return nil, fmtErr("session", err)
	}

	session, err := s.loadSession(ctx, personID)
	if err != nil {
		return nil, err
	}
	session.Token = raw
	return session, nil
}

func (s *Service) Me(ctx context.Context, rawToken string) (*Session, error) {
	rawToken = strings.TrimSpace(rawToken)
	if rawToken == "" {
		return nil, ErrUnauthorized
	}
	var personID string
	err := s.db.QueryRowContext(ctx, `
		SELECT person_id FROM auth_sessions
		WHERE token_hash = $1 AND revoked_at IS NULL AND expires_at > $2`,
		tokenHash(rawToken), s.now(),
	).Scan(&personID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrUnauthorized
	}
	if err != nil {
		return nil, err
	}
	return s.loadSession(ctx, personID)
}

func (s *Service) Logout(ctx context.Context, rawToken string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE auth_sessions SET revoked_at = $2
		WHERE token_hash = $1 AND revoked_at IS NULL`,
		tokenHash(rawToken), s.now(),
	)
	return err
}

// O TELEFONE É O CONVITE.
//
// O personal digita o número do aluno e manda o convite pelo WhatsApp dele. Nesse
// instante o número JÁ ESTÁ autorizado: quem decide quem entra no time é o dono do time,
// e ele decidiu quando digitou. Pedir o código de volta ao aluno é cobrar duas vezes pela
// mesma autorização — e é a fricção que faz alguém desistir na porta, com o app instalado
// e o convite aberto na mão.
//
// O código não morre com isto: ele continua sendo a única chave do convite ABERTO (o
// story, o cartaz na parede da academia), onde ninguém sabe de antemão qual é o telefone.
// O que ele deixa de ser é exigência para quem o personal já chamou pelo nome.
//
// A resolução mora AQUI, num lugar só, e devolve o código: tudo que já existia — a checagem
// de convite, a marca do estúdio na tela de entrada, o vínculo criado em ensurePerson —
// continua funcionando sem saber que o aluno nunca digitou nada.
func (s *Service) resolverConvite(ctx context.Context, phone, invite string) (string, error) {
	if invite != "" {
		return invite, nil
	}
	var code string
	err := s.db.QueryRowContext(ctx, `
		SELECT code FROM invites
		WHERE phone = $1 AND accepted_at IS NULL AND expires_at > $2
		ORDER BY created_at DESC
		LIMIT 1`,
		phone, s.now(),
	).Scan(&code)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmtErr("convite do telefone", err)
	}
	return code, nil
}

func (s *Service) ensurePerson(ctx context.Context, phone, invite string) (string, error) {
	var personID string
	err := s.db.QueryRowContext(ctx, `SELECT id FROM people WHERE phone = $1`, phone).Scan(&personID)
	if err == nil {
		return personID, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	if invite == "" {
		return "", ErrInviteRequired
	}

	var studioID, createdBy string
	err = s.db.QueryRowContext(ctx, `
		SELECT studio_id, created_by_person_id FROM invites
		WHERE code = $1 AND phone = $2 AND expires_at > $3`,
		invite, phone, s.now(),
	).Scan(&studioID, &createdBy)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrInviteInvalid
	}
	if err != nil {
		return "", err
	}

	err = s.db.QueryRowContext(ctx, `
		INSERT INTO people (phone, name) VALUES ($1, '') RETURNING id`,
		phone,
	).Scan(&personID)
	if err != nil {
		return "", fmtErr("create person", err)
	}

	var bondID string
	err = s.db.QueryRowContext(ctx, `
		INSERT INTO bonds (person_id, studio_id, role, status)
		VALUES ($1, $2, 'student', 'active')
		ON CONFLICT (person_id, studio_id) DO UPDATE SET status = 'active'
		RETURNING id`,
		personID, studioID,
	).Scan(&bondID)
	if err != nil {
		return "", fmtErr("bond", err)
	}

	if _, err := s.db.ExecContext(ctx, `
		UPDATE people SET active_bond_id = $2 WHERE id = $1`,
		personID, bondID,
	); err != nil {
		return "", err
	}

	if _, err := s.db.ExecContext(ctx, `
		UPDATE invites SET accepted_at = COALESCE(accepted_at, $3), accepted_person_id = $2
		WHERE code = $1`,
		invite, personID, s.now(),
	); err != nil {
		return "", err
	}

	return personID, nil
}

func (s *Service) loadSession(ctx context.Context, personID string) (*Session, error) {
	var out Session
	err := s.db.QueryRowContext(ctx, `
		SELECT p.id, p.name, p.phone, COALESCE(b.role, ''),
		       COALESCE(p.avatar_object_key, ''), COALESCE(p.avatar_color, ''),
		       COALESCE(s.id::text, ''), COALESCE(s.name, ''), COALESCE(s.accent_color, ''),
		       COALESCE(s.logo_object_key, ''), COALESCE(s.config, '{}'::jsonb),
		       COALESCE(b.onboarding ? 'experience', false),
		       b.commitment_at IS NOT NULL,
		       NOT EXISTS (
		           SELECT 1 FROM workout_sessions ws
		           WHERE ws.person_id = p.id
		             AND ws.studio_id = s.id
		             AND ws.finished_at IS NOT NULL
		       )
		FROM people p
		LEFT JOIN bonds b ON b.id = p.active_bond_id
		LEFT JOIN studios s ON s.id = b.studio_id
		WHERE p.id = $1`,
		personID,
	).Scan(
		&out.Person.ID, &out.Person.Name, &out.Person.Phone, &out.Person.Role,
		&out.Person.AvatarKey, &out.Person.AvatarColor,
		&out.Time.ID, &out.Time.Name, &out.Time.Accent, &out.Time.LogoKey, &out.Time.Config,
		&out.OnboardingComplete, &out.CommitmentComplete, &out.Debut,
	)
	if err != nil {
		return nil, fmtErr("session payload", err)
	}
	return &out, nil
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var v byte
	for i := range a {
		v |= a[i] ^ b[i]
	}
	return v == 0
}

var avatarHex = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

// PatchMe: a Pessoa edita o próprio rosto — o nome que aparece, a cor do avatar ou a
// foto (chave que ela acabou de subir pelo presign). Campos nulos ficam como estão;
// string vazia em avatar_* LIMPA o campo (voltar para as iniciais é escolha válida).
func (s *Service) PatchMe(ctx context.Context, personID string, name, avatarColor, avatarKey *string) (*Session, error) {
	if name == nil && avatarColor == nil && avatarKey == nil {
		return nil, ErrInvalid
	}
	if name != nil {
		trimmed := strings.TrimSpace(*name)
		if trimmed == "" {
			return nil, ErrInvalid
		}
		if _, err := s.db.ExecContext(ctx, `
			UPDATE people SET name = $2, updated_at = now() WHERE id = $1`,
			personID, trimmed,
		); err != nil {
			return nil, fmtErr("patch me nome", err)
		}
	}
	if avatarColor != nil {
		if *avatarColor != "" && !avatarHex.MatchString(*avatarColor) {
			return nil, ErrInvalid
		}
		if _, err := s.db.ExecContext(ctx, `
			UPDATE people SET avatar_color = NULLIF($2, ''), updated_at = now() WHERE id = $1`,
			personID, *avatarColor,
		); err != nil {
			return nil, fmtErr("patch me cor", err)
		}
	}
	if avatarKey != nil {
		// a chave é do formato que o presign gera: avatar/<person_id>/... — aceitar
		// qualquer coisa deixaria uma pessoa apontar para a foto de outra.
		if *avatarKey != "" && !strings.HasPrefix(*avatarKey, "avatar/"+personID+"/") {
			return nil, ErrInvalid
		}
		if _, err := s.db.ExecContext(ctx, `
			UPDATE people SET avatar_object_key = NULLIF($2, ''), updated_at = now() WHERE id = $1`,
			personID, *avatarKey,
		); err != nil {
			return nil, fmtErr("patch me avatar", err)
		}
	}
	return s.loadSession(ctx, personID)
}
