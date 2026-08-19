// Package pix monta o BR Code — o "copia e cola" do Pix — a partir da chave do PRÓPRIO
// personal.
//
// O dinheiro do aluno vai DIRETO para a conta dele. A LinkGym não custodia, não intermedia
// e não entra na cadeia de responsabilidade: aqui não há conta de plataforma, não há
// repasse, não há saldo. É a única forma de mover dinheiro de verdade sem que ninguém
// precise abrir subconta, mandar documento e esperar aprovação — e por isso é o que
// funciona na primeira semana, e não no primeiro trimestre.
//
// O que ele resolve, em concreto: hoje o personal abre o WhatsApp, digita a chave dele na
// mão e escreve o valor. Metade das vezes a aluna lê errado e paga R$ 300 no lugar de
// R$ 350, e aí sobra uma diferença que ninguém sabe conciliar. Com o valor DENTRO do
// código, o aplicativo do banco dela abre com o número certo e ela só confirma.
//
// Zero dependência: é uma string com campos TLV e um CRC de 16 bits.
// Referência: EMV® QRCPS MPM + Manual de Padrões para Iniciação do Pix (Banco Central).
package pix

import (
	"fmt"
	"strings"
	"unicode"
)

// Os limites são do padrão, não nossos: nome do recebedor 25, cidade 15. Estourar não dá
// erro no gerador — dá um código que alguns bancos recusam e outros truncam, que é o pior
// dos mundos para depurar.
const (
	maxNome   = 25
	maxCidade = 15
)

// Campo TLV: identificador de 2 dígitos, tamanho de 2 dígitos, valor.
func campo(id, valor string) string {
	return fmt.Sprintf("%s%02d%s", id, len(valor), valor)
}

// BRCode devolve o copia-e-cola estático COM valor.
//
// `chave` é a chave Pix do personal (CPF/CNPJ, telefone, e-mail ou aleatória); `nome` e
// `cidade` são obrigatórios pelo padrão. `cents` é o valor da cobrança — em centavos,
// como todo dinheiro deste produto.
func BRCode(chave, nome, cidade string, cents int) (string, error) {
	chave = strings.TrimSpace(chave)
	if chave == "" {
		return "", fmt.Errorf("pix: sem chave")
	}
	if cents <= 0 {
		return "", fmt.Errorf("pix: valor tem que ser positivo")
	}
	n := limpa(nome, maxNome)
	c := limpa(cidade, maxCidade)
	if n == "" || c == "" {
		return "", fmt.Errorf("pix: nome e cidade do recebedor são obrigatórios")
	}

	conta := campo("00", "br.gov.bcb.pix") + campo("01", chave)

	var b strings.Builder
	b.WriteString(campo("00", "01")) // formato do payload
	b.WriteString(campo("26", conta))
	b.WriteString(campo("52", "0000")) // categoria do estabelecimento: não se aplica
	b.WriteString(campo("53", "986"))  // moeda: BRL, ISO 4217
	b.WriteString(campo("54", fmt.Sprintf("%d.%02d", cents/100, cents%100)))
	b.WriteString(campo("58", "BR"))
	b.WriteString(campo("59", n))
	b.WriteString(campo("60", c))
	// 62-05 é o txid. Vai como "***", que é o valor do padrão para "sem referência", e não
	// um identificador nosso: um txid próprio só serviria para conciliar automaticamente,
	// e conciliar exige um provedor que nos conte que o Pix caiu — o que a fase 1 não tem.
	// Pior: o txid nem sempre sobrevive ao banco emissor, então conciliar por ele seria uma
	// promessa que quebra em silêncio. Omitir o campo 62 inteiro faz alguns bancos
	// recusarem o código; "***" é aceito em todos.
	b.WriteString(campo("62", campo("05", "***")))

	// O CRC entra por último e cobre a própria etiqueta "6304".
	semCRC := b.String() + "6304"
	return fmt.Sprintf("%s%04X", semCRC, crc16(semCRC)), nil
}

// CRC-16/CCITT-FALSE: polinômio 0x1021, inicial 0xFFFF, sem reflexão e sem XOR final. É o
// que o Manual do Pix exige, e é fácil de errar — as outras variantes de CRC-16 com o
// mesmo polinômio (XMODEM, KERMIT) dão um código que todo banco recusa. O teste ancora no
// vetor de verificação padrão: crc16("123456789") == 0x29B1.
func crc16(s string) uint16 {
	crc := uint16(0xFFFF)
	for i := 0; i < len(s); i++ {
		crc ^= uint16(s[i]) << 8
		for j := 0; j < 8; j++ {
			if crc&0x8000 != 0 {
				crc = crc<<1 ^ 0x1021
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}

// ponytail: um Replacer no lugar de golang.org/x/text/unicode/norm. A entrada é nome de
// pessoa e cidade brasileiros, e a tabela abaixo cobre o português inteiro — promover uma
// dependência (hoje indireta) para normalizar Unicode do mundo todo seria pagar caro por um
// alfabeto que a gente conhece. Se um dia entrar nome com alfabeto não latino, aí sim.
var semAcento = strings.NewReplacer(
	"Á", "A", "À", "A", "Â", "A", "Ã", "A", "Ä", "A",
	"É", "E", "È", "E", "Ê", "E", "Ë", "E",
	"Í", "I", "Ì", "I", "Î", "I", "Ï", "I",
	"Ó", "O", "Ò", "O", "Ô", "O", "Õ", "O", "Ö", "O",
	"Ú", "U", "Ù", "U", "Û", "U", "Ü", "U",
	"Ç", "C", "Ñ", "N",
)

// O padrão é ASCII maiúsculo. Acento passa pelo QR mas quebra em leitores antigos, e
// "MARIA DA CONCEIÇÃO" truncado no meio de um caractere multibyte vira lixo — então o
// acento sai ANTES de contar o tamanho.
func limpa(s string, max int) string {
	var b strings.Builder
	for _, r := range semAcento.Replace(strings.ToUpper(strings.TrimSpace(s))) {
		if r < 128 && (unicode.IsLetter(r) || unicode.IsDigit(r) || r == ' ') {
			b.WriteRune(r)
		}
	}
	out := strings.Join(strings.Fields(b.String()), " ")
	if len(out) > max {
		out = strings.TrimSpace(out[:max])
	}
	return out
}
