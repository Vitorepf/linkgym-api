package workout

import "errors"

var (
	ErrInvalid      = errors.New("invalido")
	ErrForbidden    = errors.New("nao_autorizado")
	ErrNotFound     = errors.New("nao_encontrado")
	ErrEmptySession = errors.New("sessao_vazia")
)
