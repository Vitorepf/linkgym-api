package owner

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// O CONVITE é a única porta do aluno — está escrito assim no CONTEXT.md e a tabela existe
// desde a primeira migration. O que não existia era a porta: nenhuma rota criava convite,
// e a Turma dizia "o convite é a porta de entrada" ao lado de uma lista vazia e de nenhum
// botão. O código nascia só no seed de desenvolvimento.
//
// É também o primeiro artefato da marca do personal que SAI do app: vai no WhatsApp, com
// o nome do estúdio, e é o que ele mostra quando vende. Por isso ele vive aqui, no
// domínio, e não num utilitário: quem cria convite é o dono do time, para o time dele.

// Alfabeto sem 0/O e sem 1/I/L: o código é lido em voz alta e digitado por quem está com
// o celular na mão na porta da academia. Ambiguidade de glifo aqui custa uma tentativa
// perdida e um aluno que desiste.
const alfabetoDoConvite = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

// Quatro anos. O convite não é uma promoção: é a chave da porta de um aluno que pode
// demorar semanas para instalar o app. Expirar cedo transforma a porta em suporte.
const validadeDoConvite = 4 * 365 * 24 * time.Hour

type Convite struct {
	Code      string    `json:"code"`
	Phone     string    `json:"phone,omitempty"`
	ExpiresAt time.Time `json:"expires_at"`
}

func codigoDoConvite(prefixo string) (string, error) {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	var sb strings.Builder
	for _, x := range b {
		sb.WriteByte(alfabetoDoConvite[int(x)%len(alfabetoDoConvite)])
	}
	if prefixo == "" {
		return sb.String(), nil
	}
	return prefixo + "-" + sb.String(), nil
}

// prefixoDoNome: as três primeiras letras do nome do time, em caixa alta e sem acento. O
// convite passa a ser reconhecível ("FRE-K7M2PQ") em vez de seis caracteres anônimos —
// quem recebe sabe de quem é antes de abrir o app.
func prefixoDoNome(nome string) string {
	var sb strings.Builder
	for _, r := range strings.ToUpper(nome) {
		if r >= 'A' && r <= 'Z' {
			sb.WriteRune(r)
		}
		if sb.Len() == 3 {
			break
		}
	}
	return sb.String()
}

// CriarConvite devolve o convite ABERTO daquele telefone, se já existir um, em vez de
// empilhar códigos: o personal que toca duas vezes no botão manda o mesmo link, e o aluno
// que recebeu dois não fica escolhendo qual vale. Sem telefone, o convite é aberto — serve
// para quem for.
func (s *Service) CriarConvite(ctx context.Context, ownerID, telefone string) (*Convite, error) {
	studioID, err := s.ownerTime(ctx, ownerID)
	if err != nil {
		return nil, err
	}
	telefone = strings.TrimSpace(telefone)

	if telefone != "" {
		var c Convite
		err := s.db.QueryRowContext(ctx, `
			SELECT code, phone, expires_at
			FROM invites
			WHERE studio_id = $1 AND phone = $2
			  AND accepted_at IS NULL AND expires_at > now()
			ORDER BY created_at DESC
			LIMIT 1`,
			studioID, telefone,
		).Scan(&c.Code, &c.Phone, &c.ExpiresAt)
		if err == nil {
			return &c, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("convite aberto: %w", err)
		}
	}

	var nome string
	if err := s.db.QueryRowContext(ctx, `SELECT name FROM studios WHERE id = $1`, studioID).Scan(&nome); err != nil {
		return nil, fmt.Errorf("nome do time: %w", err)
	}

	// Colisão de código é improvável (31^6) mas não impossível, e a coluna é UNIQUE: três
	// tentativas em vez de um erro na cara do personal.
	for i := 0; i < 3; i++ {
		code, err := codigoDoConvite(prefixoDoNome(nome))
		if err != nil {
			return nil, err
		}
		expira := s.now().Add(validadeDoConvite)
		_, err = s.db.ExecContext(ctx, `
			INSERT INTO invites (studio_id, created_by_person_id, phone, code, expires_at)
			VALUES ($1, $2, $3, $4, $5)`,
			studioID, ownerID, telefone, code, expira,
		)
		if err == nil {
			return &Convite{Code: code, Phone: telefone, ExpiresAt: expira}, nil
		}
		if !strings.Contains(err.Error(), "invites_code_key") {
			return nil, fmt.Errorf("criar convite: %w", err)
		}
	}
	return nil, fmt.Errorf("criar convite: três colisões de código seguidas")
}
