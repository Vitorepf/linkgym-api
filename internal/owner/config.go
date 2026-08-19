package owner

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"

	"github.com/Vitorepf/linkgym-api/internal/voz"
)

// A CONFIG do app do personal — o cardápio inteiro, com trilhos. Cada campo é opcional
// no PATCH; o documento gravado é sempre o objeto completo validado. Nenhum campo fora
// desta struct entra no banco: liberdade em cardápio, nunca em texto/valor livre.
type TimeConfig struct {
	// liga: "nomes" (ranking aberto), "anonima" (só Você é nomeado), "off".
	Liga *string `json:"liga,omitempty"`
	// selos e xp: visíveis para o aluno. Liga ligada exige XP visível — ranking
	// ordenado por número invisível é incoerência que a fábrica não permite.
	Selos *bool `json:"selos,omitempty"`
	XP    *bool `json:"xp,omitempty"`
	// prontidão diária (energia/dor/sono) ligada?
	Prontidao *bool `json:"prontidao,omitempty"`
	// passo do ajuste de carga na série: 0.5, 1 ou 2.5 kg.
	PassoKg *float64 `json:"passo_kg,omitempty"`
	// dias por semana pré-selecionados no Compromisso (o aluno ainda confirma).
	DiasPadrao *int `json:"dias_padrao,omitempty"`
	// A VOZ: slots curtos, sem URL/telefone (a porta de entrada não vira panfleto).
	BoasVindas *string `json:"boas_vindas,omitempty"`
	Retomada   *string `json:"retomada,omitempty"`
	// A APARÊNCIA: o app inteiro na mão do personal. Cardápio fechado em todos os campos,
	// e é isso que faz a promessa "customização total, resultado sempre premium" ser
	// verificável em vez de otimista: o app percorre o produto cartesiano inteiro
	// (tools/aparencia.mjs) e mede contraste, alvo de dedo e separação de cores em cada
	// combinação ANTES de existir botão para escolhê-la. Valor fora do cardápio não grava.
	// A cor primária NÃO mora aqui: ela é a coluna studios.accent_color, que viaja no
	// convite, onde config não vai. Documento com dois donos da mesma cor diverge.
	Aparencia *Aparencia `json:"aparencia,omitempty"`
}

type Aparencia struct {
	Chao       *string `json:"chao,omitempty"`
	Voz        *string `json:"voz,omitempty"`
	Acao       *string `json:"acao,omitempty"`
	Hierarquia *string `json:"hierarquia,omitempty"`
	Anel       *string `json:"anel,omitempty"`
	Numero     *string `json:"numero,omitempty"`
	Contraste  *string `json:"contraste,omitempty"`
	Secundaria *string `json:"secundaria,omitempty"`
	Forma      *string `json:"forma,omitempty"`
	Superficie *string `json:"superficie,omitempty"`
	Peso       *string `json:"peso,omitempty"`
	Densidade  *string `json:"densidade,omitempty"`
	Movimento  *string `json:"movimento,omitempty"`
}

var cardapioDaAparencia = map[string][]string{
	"chao": {"carvao", "breu", "grafite", "tabaco", "papel", "neve", "linho"},
	// A voz é um PAR de fontes curado, nunca uma fonte livre: "escolha a fonte" é o pedido
	// que produz Comic Sans em produto sério.
	"voz": {"bloco", "neutra", "tecnica", "editorial", "suave", "condensada"},
	// O estilo do botão é ANATOMIA, não acabamento: onde o rótulo mora dentro da peça.
	"acao": {"linha", "centro", "caixa", "empilhada"},
	// O peso do PAR de ações. O peso do primário não é escolha: contorno ou tinta-só no
	// botão principal joga fora a garantia de massa e matiz, e nas dez telas em que os
	// dois botões dividem a tela primário e secundário viram o mesmo objeto.
	"hierarquia": {"salto", "parelha", "eco"},
	// O anel da ação cheia deixa de ser loteria da marca e vira decisão declarada.
	"anel": {"resgate", "sempre"},
	// A anatomia de uma cifra: o mesmo par número/rótulo em três ordens.
	"numero": {"empilhado", "linha", "cartaz"},
	// A força das seis tintas contra os quatro fundos. Só SOBE.
	"contraste":  {"normal", "alto"},
	"forma":      {"reta", "macia", "pilula"},
	"superficie": {"solida", "contorno", "elevada", "vidro"},
	"peso":       {"fino", "medio", "grosso"},
	"densidade":  {"compacta", "normal", "arejada"},
	"movimento":  {"seco", "normal", "generoso"},
}

var hexDaCor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func noCardapio(campo string, v *string) error {
	if v == nil {
		return nil
	}
	for _, ok := range cardapioDaAparencia[campo] {
		if *v == ok {
			return nil
		}
	}
	return ErrInvalid
}

func validaAparencia(a *Aparencia) error {
	if a == nil {
		return nil
	}
	campos := map[string]*string{
		"voz": a.Voz, "acao": a.Acao, "forma": a.Forma, "superficie": a.Superficie,
		"peso": a.Peso, "densidade": a.Densidade, "movimento": a.Movimento,
		"hierarquia": a.Hierarquia, "anel": a.Anel, "numero": a.Numero, "contraste": a.Contraste,
	}
	for campo, v := range campos {
		if err := noCardapio(campo, v); err != nil {
			return err
		}
	}
	// O CHÃO é o segundo campo contínuo, e a exceção tem dono: os sete nomes continuam
	// sendo dado medido, e um hex livre é resolvido por `escada()` no app, que deriva os
	// outros nove degraus até bater os pisos de contraste. A janela de claridade em que a
	// escada cabe NÃO é enforçada aqui de propósito: ela é conta de L*, e ter essa conta em
	// duas linguagens é a divergência que este produto mais paga caro. Quem está fora da
	// janela ANDA para dentro dela, dentro do próprio solver — nenhum chão que ninguém
	// mediu chega a ser representável, e o servidor não precisa saber por quê.
	if a.Chao != nil && !hexDaCor.MatchString(*a.Chao) {
		if err := noCardapio("chao", a.Chao); err != nil {
			return err
		}
	}
	// A segunda cor é o único campo contínuo, e mesmo ela só aceita "auto" ou um hex de
	// seis dígitos: o app deriva a legibilidade dela, mas não consegue derivar de lixo.
	if a.Secundaria != nil && *a.Secundaria != "auto" && !hexDaCor.MatchString(*a.Secundaria) {
		return ErrInvalid
	}
	return nil
}

// Porta pública da validação: o handler precisa recusar ANTES de gravar o nome e a cor,
// que são a outra metade do mesmo PATCH.
func ValidarConfig(c *TimeConfig) error { return validaConfig(c) }

func validaConfig(c *TimeConfig) error {
	if c.Liga != nil {
		switch *c.Liga {
		case "nomes", "anonima", "off":
		default:
			return ErrInvalid
		}
	}
	if c.PassoKg != nil && *c.PassoKg != 0.5 && *c.PassoKg != 1 && *c.PassoKg != 2.5 {
		return ErrInvalid
	}
	if c.DiasPadrao != nil && (*c.DiasPadrao < 2 || *c.DiasPadrao > 6) {
		return ErrInvalid
	}
	// A regra das frases e uma so, e mora em internal/voz: a linha que o personal escreve
	// ao publicar obedece a MESMA, pelo MESMO caminho.
	if c.BoasVindas != nil {
		s, ok := voz.Limpa(*c.BoasVindas, 100)
		if !ok {
			return ErrInvalid
		}
		*c.BoasVindas = s
	}
	if c.Retomada != nil {
		s, ok := voz.Limpa(*c.Retomada, 90)
		if !ok {
			return ErrInvalid
		}
		*c.Retomada = s
	}
	if err := validaAparencia(c.Aparencia); err != nil {
		return err
	}
	// o acoplamento que impede configuração incoerente: liga precisa de XP visível.
	if c.Liga != nil && *c.Liga != "off" && c.XP != nil && !*c.XP {
		return ErrInvalid
	}
	return nil
}

// PatchTimeConfig substitui a config inteira (o app manda o documento completo — menos
// estado de merge, menos surpresa). Validação por whitelist; o que não é do cardápio
// não grava.
func (s *Service) PatchTimeConfig(ctx context.Context, ownerID string, cfg *TimeConfig) error {
	if cfg == nil {
		return ErrInvalid
	}
	if err := validaConfig(cfg); err != nil {
		return err
	}
	studioID, err := s.ownerTime(ctx, ownerID)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("config marshal: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `
		UPDATE studios SET config = $2, updated_at = now() WHERE id = $1`,
		studioID, raw,
	); err != nil {
		return fmt.Errorf("patch config: %w", err)
	}
	return nil
}
