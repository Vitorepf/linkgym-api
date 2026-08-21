// Package pagamento é a fronteira entre o produto e o dinheiro que passa por dentro.
//
// # O DESENHO, e por que ele NÃO é "criar o produto na conta Stripe do LinkGym"
//
// A ideia intuitiva — o personal cria um produto e a gente cria o mesmo produto na NOSSA
// conta, recebe o dinheiro e repassa — é a que fecha a operação. Ela faz o LinkGym ser o
// merchant of record de tudo: juridicamente NÓS vendemos o suplemento, a marmita e a sessão
// de treino, com a responsabilidade de entrega e devolução que vem junto; a receita bruta de
// todos os personais entra como receita nossa e sai como despesa; chargeback e MED do Pix
// batem na nossa conta; e agregar pagamento de terceiros numa conta única viola o próprio
// contrato de uso de qualquer adquirente — é o jeito clássico de a conta ser congelada com o
// dinheiro dos personais dentro.
//
// O desenho correto é o oposto: CADA PERSONAL TEM A PRÓPRIA CONTA no provedor, a cobrança é
// emitida por ela, o dinheiro nasce lá, e a plataforma retira só a taxa no ato do repasse. Nós
// somos plataforma, nunca vendedor, e nunca custodiamos.
//
// # POR QUE NÃO STRIPE (verificado)
//
// Stripe Connect existe no Brasil e aceita conta conectada brasileira. Mas o Pix na Stripe,
// para empresa sediada no Brasil, é por convite — e Pix é o trilho principal deste produto,
// porque o cliente final é aluno de academia. Sem Pix sobra cartão, a 3,99% + R$0,39 e
// liquidando em D+30, para quem muitas vezes não tem limite sobrando. E não existe
// recorrência em Pix na Stripe.
//
// Por isso a interface abaixo é um PROVEDOR, e não "a Stripe": quem entra primeiro é um
// provedor nacional com repasse nativo, subconta por API e Pix, e a escolha não vaza para o
// resto do código.
//
// # O QUE ESTE PACOTE FAZ HOJE
//
// Ele define a fronteira e implementa o provedor que já funciona sem ninguém aprovar nada: o
// Pix DIRETO, em que o dinheiro vai da aluna para a conta do personal sem passar por lugar
// nenhum e a taxa é ZERO. Isso não é um stub — é o caminho que move dinheiro na primeira
// semana, e é o que a Fase 1 usa. O provedor com split entra depois, implementando a mesma
// interface, e nenhuma tela muda.
package pagamento

import (
	"context"
	"errors"
	"fmt"
)

// A TAXA DA PLATAFORMA.
//
// Percentual com TETO, e o teto é a decisão que importa: o custo do provedor por transação é
// FIXO (da ordem de R$ 2 por Pix), enquanto uma taxa percentual cresce sem limite. Sem teto,
// uma mensalidade de R$ 1.500 pagaria R$ 30 para um serviço que custou R$ 2 — e o personal
// que fatura mais é exatamente quem tem margem e motivo para levar a cobrança para o Pix
// pessoal dele. Desintermediação é o risco número 1 deste modelo, e ela não se combate com
// cláusula de contrato; se combate fazendo a conta não valer a pena.
//
// 1,99% com teto de R$ 14,90: abaixo do teto observado no mercado brasileiro de gestão para
// personal, e acima do custo do provedor em toda faixa realista.
const (
	TaxaBasisPoints = 199  // 1,99%
	TaxaTetoCents   = 1490 // R$ 14,90
)

// Taxa calcula o que a plataforma retém, em centavos.
//
// ARREDONDA PARA BAIXO, e quem come o centavo somos nós. É meio centavo por transação e
// nunca aparece no extrato de ninguém; arredondar para cima faria o personal receber um
// centavo a menos do que a conta dele diz, e é o tipo de diferença que ele encontra e não
// esquece.
func Taxa(valorCents int) int {
	if valorCents <= 0 {
		return 0
	}
	t := valorCents * TaxaBasisPoints / 10000
	if t > TaxaTetoCents {
		return TaxaTetoCents
	}
	return t
}

// Ordem é o que se pede ao provedor: quanto, de quem, e com que chave de idempotência.
//
// NÃO se chama "cobrança", e não é preciosismo: o produto recusa o vocabulário de sistema de
// faturamento inteiro (cobrança, checkout, gateway, boleto, split, inadimplente) porque o app
// nunca cobra o aluno — quem cobra é o personal, com a frase dele. A catraca de vocabulário
// reprova o nome, e ela está certa: o nome que a gente dá para uma coisa é o que a próxima
// pessoa vai construir em cima dela. `ContaDoRecebedor` é a conta DO PERSONAL: o dinheiro
// nasce lá, e é isso que mantém a plataforma fora da cadeia de custódia.
type Ordem struct {
	ContaDoRecebedor string
	ValorCents       int
	TaxaCents        int
	Descricao        string
	// Chave de idempotência do NOSSO lado. Webhook chega duas vezes e requisição repete;
	// sem esta chave a segunda vira uma segunda linha na fatura da aluna.
	Chave string
}

// Emitida é o que o provedor devolve.
type Emitida struct {
	// id no provedor, que é por onde a conciliação acontece depois.
	ProvedorID string
	// o copia-e-cola ou a URL que a aluna usa.
	CopiaECola string
	// quanto de fato foi retido: pode divergir do pedido se o provedor arredondar
	// diferente, e a verdade é a DELE — o extrato que o personal confere é o do provedor.
	TaxaCents int
}

var (
	// ErrSemConta: o personal ainda não abriu a conta no provedor. Não é falha do produto —
	// é o degrau de KYC, e é onde a adoção morre se for pedido cedo demais.
	ErrSemConta = errors.New("sem conta no provedor")
	// ErrNaoSuportado: o provedor atual não faz isso (o Pix direto não emite recorrência).
	ErrNaoSuportado = errors.New("nao suportado por este provedor")
)

// Provedor é a fronteira. Trocar de provedor — ou ter dois, um por país — é implementar
// isto de novo, e nenhuma tela sabe qual está ligado.
type Provedor interface {
	// Nome, para o registro e para o extrato.
	Nome() string
	// Emitir uma ordem avulsa.
	Emitir(ctx context.Context, c Ordem) (*Emitida, error)
	// Assinar: a ordem que repete todo mês. O Pix direto não faz — e é a razão principal
	// de existir uma Fase 2.
	Assinar(ctx context.Context, c Ordem, diaDoVencimento int) (*Emitida, error)
	// CustodiaODinheiro diz se o dinheiro passa por uma conta da plataforma. Tem que ser
	// FALSE em qualquer provedor que a gente aceite: true significa que somos merchant of
	// record, com a responsabilidade solidária e a obrigação fiscal que vêm junto.
	CustodiaODinheiro() bool
}

// PixDireto é o provedor que funciona sem ninguém aprovar nada.
//
// O BR Code é montado a partir da chave do próprio personal; a aluna paga; o dinheiro cai na
// conta dele. A plataforma não vê a transação, não custodia e não cobra nada — a taxa é ZERO
// aqui, e isso é honesto: não há serviço prestado para cobrar.
//
// Ele não é um degrau descartável: continua sendo a saída para quem não quer abrir subconta,
// e é o que mantém o produto útil ANTES do KYC — que é a única forma de o personal já estar
// dentro no dia em que a gente pedir o documento.
type PixDireto struct {
	// Monta o BR Code. Injetado para este pacote não depender de internal/pix e o teste
	// não precisar de chave de ninguém.
	MontarBRCode func(chave, nome, cidade string, cents int) (string, error)
	Chave        string
	Recebedor    string
	Cidade       string
}

func (p PixDireto) Nome() string { return "pix_direto" }

func (p PixDireto) CustodiaODinheiro() bool { return false }

func (p PixDireto) Emitir(_ context.Context, c Ordem) (*Emitida, error) {
	if p.Chave == "" {
		return nil, ErrSemConta
	}
	codigo, err := p.MontarBRCode(p.Chave, p.Recebedor, p.Cidade, c.ValorCents)
	if err != nil {
		return nil, fmt.Errorf("pix direto: %w", err)
	}
	// TAXA ZERO, e não a taxa da tabela: o dinheiro não passou por dentro, então não há o
	// que reter. Cobrar aqui seria a plataforma faturando sobre um serviço que não prestou.
	return &Emitida{ProvedorID: "", CopiaECola: codigo, TaxaCents: 0}, nil
}

func (p PixDireto) Assinar(context.Context, Ordem, int) (*Emitida, error) {
	// Pix estático não repete sozinho. A recorrência exige um provedor que agende a
	// ordem — e fingir que existe aqui seria o produto prometendo à aluna um débito que
	// nunca vai acontecer.
	return nil, ErrNaoSuportado
}
