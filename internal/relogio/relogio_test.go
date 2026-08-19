package relogio_test

import (
	"testing"
	"time"

	"github.com/Vitorepf/linkgym-api/internal/relogio"
)

// O DEFEITO, em forma de teste: 31/08 às 21h30 em São Paulo já é 01/09 em UTC. Com o mês
// montado sobre a hora da máquina (contêiner Alpine sem tzdata = UTC), a competência
// pulava para setembro três horas antes da virada — a turma inteira voltava a dever, e um
// toque em "Recebi" nessa janela gravava no mês errado, deixando agosto em aberto para
// sempre.
func TestCompetenciaNaViradaDoMes(t *testing.T) {
	casos := []struct {
		nome string
		utc  string
		quer string
	}{
		{"21h30 do dia 31 ainda é agosto", "2026-09-01T00:30:00Z", "2026-08-01"},
		{"23h59 do dia 31 ainda é agosto", "2026-09-01T02:59:00Z", "2026-08-01"},
		{"00h01 do dia 1º já é setembro", "2026-09-01T03:01:00Z", "2026-09-01"},
		{"meio do mês não tem truque", "2026-08-15T18:00:00Z", "2026-08-01"},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			at, err := time.Parse(time.RFC3339, c.utc)
			if err != nil {
				t.Fatal(err)
			}
			if got := relogio.Competencia(at); got != c.quer {
				t.Fatalf("Competencia(%s) = %s, queria %s", c.utc, got, c.quer)
			}
		})
	}
}

// O mesmo defeito no dia civil: a ficha "de hoje" do aluno virava às 21h.
func TestDiaViraAMeiaNoiteDeBrasilia(t *testing.T) {
	casos := []struct{ utc, quer string }{
		{"2026-08-19T23:00:00Z", "2026-08-19"}, // 20h em SP: ainda é dia 19
		{"2026-08-20T02:59:00Z", "2026-08-19"}, // 23h59 em SP: ainda é dia 19
		{"2026-08-20T03:00:00Z", "2026-08-20"}, // meia-noite em SP: virou
	}
	for _, c := range casos {
		at, _ := time.Parse(time.RFC3339, c.utc)
		if got := relogio.Dia(at); got != c.quer {
			t.Fatalf("Dia(%s) = %s, queria %s", c.utc, got, c.quer)
		}
	}
}

// O fuso tem que resolver num contêiner sem tzdata instalado — é para isso que o pacote
// importa time/tzdata. Se este teste passar aqui e o binário quebrar em produção, a causa
// é alguém ter removido aquele import.
func TestFusoResolveSemTzdataNoSistema(t *testing.T) {
	if relogio.Fuso.String() != "America/Sao_Paulo" {
		t.Fatalf("fuso = %s", relogio.Fuso)
	}
	// -03 o ano inteiro desde o fim do horário de verão brasileiro (2019).
	_, off := time.Date(2026, 1, 15, 12, 0, 0, 0, relogio.Fuso).Zone()
	if off != -3*3600 {
		t.Fatalf("offset em janeiro = %d, queria -10800", off)
	}
}
