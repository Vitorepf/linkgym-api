package pix

import (
	"strings"
	"testing"
)

// O CRC é a parte fácil de errar: as outras variantes de CRC-16 com o mesmo polinômio
// (XMODEM, KERMIT) produzem um código que TODO banco recusa, e o sintoma é "não funciona",
// sem nenhuma pista. Este é o vetor de verificação padrão do CRC-16/CCITT-FALSE.
func TestCRCNoVetorPadrao(t *testing.T) {
	if got := crc16("123456789"); got != 0x29B1 {
		t.Fatalf("crc16(\"123456789\") = %04X, queria 29B1", got)
	}
}

// O código montado, campo a campo. Se alguém mexer na ordem ou no tamanho, o teste diz
// exatamente onde — e o CRC no fim confere o todo.
func TestBRCodeCompleto(t *testing.T) {
	got, err := BRCode("fred@studio.com.br", "Fred Personal", "Sao Paulo", 35000)
	if err != nil {
		t.Fatal(err)
	}
	quer := []string{
		"000201",                 // formato
		"0014br.gov.bcb.pix",     // GUI dentro do 26
		"0118fred@studio.com.br", // a chave
		"52040000",               // categoria
		"5303986",                // BRL
		"5406350.00",             // o VALOR: é ele que impede a aluna pagar 300
		"5802BR",
		"5913FRED PERSONAL",
		"6009SAO PAULO",
		"62070503***",
	}
	for _, q := range quer {
		if !strings.Contains(got, q) {
			t.Fatalf("faltou %q em:\n%s", q, got)
		}
	}
	if !strings.HasPrefix(got, "00020126") {
		t.Fatalf("prefixo errado: %s", got)
	}
	// O CRC cobre a própria etiqueta 6304 e fecha o código.
	corpo, crc := got[:len(got)-4], got[len(got)-4:]
	if !strings.HasSuffix(corpo, "6304") {
		t.Fatalf("etiqueta do CRC ausente: %s", got)
	}
	if esperado := crc16(corpo); crc != strings.ToUpper(hex4(esperado)) {
		t.Fatalf("crc %s, esperava %s", crc, hex4(esperado))
	}
}

func hex4(v uint16) string {
	const d = "0123456789ABCDEF"
	return string([]byte{d[v>>12&0xF], d[v>>8&0xF], d[v>>4&0xF], d[v&0xF]})
}

// Acento e tamanho: o padrão é ASCII e os limites são 25 e 15. Cortar depois de tirar o
// acento é o que impede o corte cair no meio de um caractere multibyte e virar lixo.
func TestNomeECidadeCabemNoPadrao(t *testing.T) {
	got, err := BRCode("+5511999999999", "Conceição de Assunção Gonçalves", "São Bernardo do Campo", 12345)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "5925CONCEICAO DE ASSUNCAO G") {
		t.Fatalf("nome nao normalizou/coube: %s", got)
	}
	if !strings.Contains(got, "6015SAO BERNARDO DO") {
		t.Fatalf("cidade nao normalizou/coube: %s", got)
	}
	if !strings.Contains(got, "5406123.45") {
		t.Fatalf("centavos errados: %s", got)
	}
	for i := 0; i < len(got); i++ {
		if got[i] > 127 {
			t.Fatalf("byte nao-ASCII na posicao %d: %s", i, got)
		}
	}
}

// Recusa antes de gerar: um código sem chave, sem valor ou sem recebedor é um código que o
// banco rejeita depois — e aí o personal descobre no meio da conversa com a aluna.
func TestRecusaOQueOBancoRecusaria(t *testing.T) {
	casos := []struct {
		nome              string
		chave, quem, onde string
		cents             int
	}{
		{"sem chave", "", "Fred", "Sao Paulo", 100},
		{"valor zero", "k", "Fred", "Sao Paulo", 0},
		{"valor negativo", "k", "Fred", "Sao Paulo", -50},
		{"sem nome", "k", "", "Sao Paulo", 100},
		{"nome so de acento", "k", "!!!", "Sao Paulo", 100},
		{"sem cidade", "k", "Fred", "", 100},
	}
	for _, c := range casos {
		if _, err := BRCode(c.chave, c.quem, c.onde, c.cents); err == nil {
			t.Fatalf("%s: gerou codigo invalido", c.nome)
		}
	}
}
