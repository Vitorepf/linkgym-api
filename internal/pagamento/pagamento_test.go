package pagamento

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

// A TAXA, e o TETO. O teto é a decisão que importa: o custo do provedor é fixo (~R$2 por
// transação) e uma taxa percentual cresce sem limite — sem teto, quem fatura mais é
// exatamente quem tem margem e motivo para levar a cobrança para o Pix pessoal.
// Desintermediação não se combate com cláusula; se combate fazendo a conta não valer a pena.
func TestTaxaTemTeto(t *testing.T) {
	casos := []struct{ valor, quer int }{
		{0, 0},
		{-100, 0},
		{1000, 19},      // R$ 10,00 -> R$ 0,19
		{35000, 696},    // R$ 350,00 -> R$ 6,96
		{74874, 1489},   // um centavo abaixo do ponto em que o teto morde
		{74875, 1490},   // e o primeiro valor que bate no teto: R$ 748,75
		{150000, 1490},  // R$ 1.500 -> teto, e nao R$ 29,85
		{9999900, 1490}, // qualquer coisa acima continua no teto
	}
	for _, c := range casos {
		if got := Taxa(c.valor); got != c.quer {
			t.Errorf("Taxa(%d) = %d, queria %d", c.valor, got, c.quer)
		}
	}
	// ARREDONDA PARA BAIXO: o centavo é nosso, nunca do personal. Um centavo a menos no
	// extrato dele é o tipo de diferença que ele encontra e não esquece.
	if got := Taxa(101); got != 2 {
		t.Errorf("Taxa(101) = %d; 1,99%% de 101 e 2,0099, e o piso e 2", got)
	}
}

// O PIX DIRETO cobra ZERO, e isso é a regra, não um esquecimento: o dinheiro vai da aluna
// direto para a conta do personal, a plataforma não vê a transação e não presta serviço
// nenhum ali. Cobrar seria faturar sobre o que não se fez.
func TestPixDiretoNaoCustodiaENaoCobra(t *testing.T) {
	p := PixDireto{
		MontarBRCode: func(chave, nome, cidade string, cents int) (string, error) {
			return fmt.Sprintf("BR:%s:%d", chave, cents), nil
		},
		Chave: "fred@studio.com.br", Recebedor: "Fred", Cidade: "Sao Paulo",
	}
	if p.CustodiaODinheiro() {
		t.Fatal("o pix direto NAO pode custodiar: e o ponto inteiro dele")
	}
	got, err := p.Emitir(context.Background(), Ordem{ValorCents: 35000, TaxaCents: Taxa(35000)})
	if err != nil {
		t.Fatal(err)
	}
	if got.TaxaCents != 0 {
		t.Fatalf("o pix direto reteve %d centavos", got.TaxaCents)
	}
	if got.CopiaECola != "BR:fred@studio.com.br:35000" {
		t.Fatalf("codigo %q", got.CopiaECola)
	}

	// Sem chave, o erro é o degrau de KYC — não uma falha do produto.
	semChave := PixDireto{MontarBRCode: p.MontarBRCode}
	if _, err := semChave.Emitir(context.Background(), Ordem{ValorCents: 100}); !errors.Is(err, ErrSemConta) {
		t.Fatalf("sem chave devia ser ErrSemConta, veio %v", err)
	}

	// E ele NÃO finge que assina: Pix estático não repete sozinho, e prometer à aluna um
	// débito que nunca vai acontecer é pior que dizer que não dá.
	if _, err := p.Assinar(context.Background(), Ordem{ValorCents: 100}, 5); !errors.Is(err, ErrNaoSuportado) {
		t.Fatalf("assinar devia ser ErrNaoSuportado, veio %v", err)
	}
}

// A INTERFACE é a fronteira: qualquer provedor tem que responder que NÃO custodia. Um
// provedor que custodia faz a plataforma virar merchant of record — com responsabilidade
// solidária, obrigação fiscal sobre receita que não é dela, e chargeback na conta errada.
func TestProvedorNaoPodeCustodiar(t *testing.T) {
	var p Provedor = PixDireto{MontarBRCode: func(string, string, string, int) (string, error) {
		return "", nil
	}}
	if p.CustodiaODinheiro() {
		t.Fatalf("%s custodia o dinheiro: recusado por desenho", p.Nome())
	}
}
