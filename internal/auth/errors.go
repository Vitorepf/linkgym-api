package auth

import "errors"

var (
	ErrPhoneInvalid   = errors.New("telefone_invalido")
	ErrInviteRequired = errors.New("convite_obrigatorio")
	ErrInviteInvalid  = errors.New("convite_invalido")
	ErrCodeInvalid    = errors.New("codigo_invalido")
	ErrUnauthorized   = errors.New("nao_autorizado")
)

// ErrInvalid: entrada do PATCH /v1/me que não passa na validação (nome vazio, hex torto,
// chave de avatar fora do prefixo da própria pessoa).
var ErrInvalid = errors.New("invalido")
