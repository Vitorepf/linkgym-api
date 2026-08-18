# LinkGym — acesso (telefone + OTP)

Data: 2026-08-18  
Status: ativo

## Decisão

A Pessoa é o **telefone**. Não é o iPhone, não é o e-mail, não é o convite.

Igual iFood/Uber: o número recebe 4 dígitos, qualquer aparelho. Perdeu o celular → mesmo número, OTP novo, entra. Sessões antigas continuam até expirar ou a pessoa sair.

E-mail é da Pessoa, opcional, **depois**. Não trava o v1. Não é chave de login agora.

Sem senha.

## Portas

| Quem | Primeira vez | Depois |
| --- | --- | --- |
| Aluno | telefone + convite + OTP | telefone + OTP |
| Personal | telefone + OTP (já tem time) | telefone + OTP |

Sem convite válido, número novo não entra. Não existe aluno órfão.

## Aparelho

Não amarramos a conta no hardware. O OTP *é* a prova de que o número está com a pessoa.

## Desenvolvimento

`ENV=development`: OTP é sempre `0000`. A API devolve `dev_code` e loga o código. O app mostra atalho Fred / Vitor / Huan / Jose.

Em produção isso não existe. SMS entra quando o fluxo já estiver estável.

## HTTP

- `POST /v1/auth/code` `{ phone, invite_code? }`
- `POST /v1/auth/verify` `{ phone, code, invite_code? }` → `{ token, person, studio }`
- `GET /v1/me` `Authorization: Bearer`
- `POST /v1/auth/logout`
