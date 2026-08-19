package owner

import (
	"errors"
	"strings"
	"testing"
)

// A regra das frases passou a morar em internal/voz, porque a linha que o personal escreve
// ao publicar obedece a MESMA e os dois pacotes nao podiam importar um ao outro. Refactor
// de validacao sem teste e rede que ninguem ve romper: estes dois slots continuam presos
// no mesmo fio, e este teste e quem avisa se alguem o cortar.
func TestVozDaConfigRecusaLinkTelefoneEExcesso(t *testing.T) {
	texto := func(s string) *string { return &s }

	for _, frase := range []string{
		"Bora! www.meusite.test",
		"Fecha em https://exemplo.test/vendas",
		"Me chama no wa.me/5511999998888",
		"Liga +5511999998888",
		"Whats 11999998888",
	} {
		if err := ValidarConfig(&TimeConfig{BoasVindas: texto(frase)}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("boas_vindas %q: err %v want ErrInvalid", frase, err)
		}
		if err := ValidarConfig(&TimeConfig{Retomada: texto(frase)}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("retomada %q: err %v want ErrInvalid", frase, err)
		}
	}

	// Cada slot tem o seu teto, e o teto conta RUNA: a frase e em portugues.
	for _, caso := range []struct {
		nome  string
		teto  int
		monta func(*string) *TimeConfig
	}{
		{"boas_vindas", 100, func(s *string) *TimeConfig { return &TimeConfig{BoasVindas: s} }},
		{"retomada", 90, func(s *string) *TimeConfig { return &TimeConfig{Retomada: s} }},
	} {
		demais := strings.Repeat("ã", caso.teto+1)
		if err := ValidarConfig(caso.monta(&demais)); !errors.Is(err, ErrInvalid) {
			t.Fatalf("%s com %d runas: err %v want ErrInvalid", caso.nome, caso.teto+1, err)
		}
		cabe := strings.Repeat("ã", caso.teto)
		if err := ValidarConfig(caso.monta(&cabe)); err != nil {
			t.Fatalf("%s com %d runas: %v", caso.nome, caso.teto, err)
		}
	}

	// E a frase boa entra aparada, porque o espaco sobrando vira recuo na tela do aluno.
	c := &TimeConfig{BoasVindas: texto("  Montei esse começo para você.  ")}
	if err := ValidarConfig(c); err != nil {
		t.Fatal(err)
	}
	if *c.BoasVindas != "Montei esse começo para você." {
		t.Fatalf("boas_vindas %q nao foi aparada", *c.BoasVindas)
	}
}
