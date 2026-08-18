package auth

import "errors"

var (
	ErrPhoneInvalid    = errors.New("telefone_invalido")
	ErrInviteRequired  = errors.New("convite_obrigatorio")
	ErrInviteInvalid   = errors.New("convite_invalido")
	ErrCodeInvalid     = errors.New("codigo_invalido")
	ErrUnauthorized    = errors.New("nao_autorizado")
)
