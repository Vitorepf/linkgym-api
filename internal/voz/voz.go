// Package voz guarda a UNICA regra das frases que o personal escreve e o aluno le.
//
// Sao tres slots hoje — boas_vindas e retomada (studios.config) e a linha da Prescricao —
// e uma regra so: cabe no limite, e nem link nem telefone. A porta de entrada do aluno nao
// vira panfleto, e o slot que existe para soar como pessoa nao vira canal de venda.
//
// Mora em pacote proprio, e nao junto de um dos dois donos, porque owner ja importa
// publish: a regra na casa de qualquer um deles obrigaria o outro a copia-la. Duas copias
// da mesma regra sao duas regras no dia em que alguem mexer numa so — e o dia em que a
// copia esquecida deixar passar um link, quem paga e a pessoa cujo nome esta na frase.
package voz

import (
	"regexp"
	"strings"
)

var linkOuFone = regexp.MustCompile(`(?i)(https?://|www\.|wa\.me|\+?\d{8,})`)

// Limpa apara a frase e diz se ela vale.
//
// Frase vazia SEMPRE vale, e isso e desenho, nao descuido: a ausencia e o que faz o app
// calar em vez de assinar em nome de quem nao escreveu. Quem trata o vazio e a tela.
func Limpa(s string, max int) (string, bool) {
	s = strings.TrimSpace(s)
	return s, len([]rune(s)) <= max && !linkOuFone.MatchString(s)
}
