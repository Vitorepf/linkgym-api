package owner

import "testing"

func ptr(s string) *string { return &s }

// A promessa da fábrica de aparência é "customização total, resultado sempre premium", e
// ela só se sustenta porque cada campo é CARDÁPIO FECHADO: o app percorre o produto
// cartesiano inteiro e mede contraste, alvo de dedo e separação de cores em cada
// combinação ANTES de existir botão para escolhê-la (`node tools/aparencia.mjs`, 12 mil
// pares). Um valor fora do cardápio entrando no banco é uma aparência que ninguém mediu —
// e é exatamente o que esta whitelist existe para impedir.
func TestAparenciaRecusaValorForaDoCardapio(t *testing.T) {
	fora := []struct {
		nome string
		a    Aparencia
	}{
		{"chão inventado", Aparencia{Chao: ptr("neon")}},
		{"chão que não é hex", Aparencia{Chao: ptr("#12345")}},
		{"hierarquia inventada", Aparencia{Hierarquia: ptr("gritante")}},
		{"anel inventado", Aparencia{Anel: ptr("duplo")}},
		{"número inventado", Aparencia{Numero: ptr("relogio")}},
		{"contraste inventado", Aparencia{Contraste: ptr("baixo")}},
		{"voz inventada", Aparencia{Voz: ptr("comic")}},
		{"ação inventada", Aparencia{Acao: ptr("flutuante")}},
		{"forma inventada", Aparencia{Forma: ptr("estrela")}},
		{"superfície inventada", Aparencia{Superficie: ptr("gradiente")}},
		{"peso inventado", Aparencia{Peso: ptr("gigante")}},
		{"densidade inventada", Aparencia{Densidade: ptr("apertadissima")}},
		{"movimento inventado", Aparencia{Movimento: ptr("elastico")}},
		{"segunda cor que não é cor", Aparencia{Secundaria: ptr("azul")}},
		{"segunda cor com três dígitos", Aparencia{Secundaria: ptr("#abc")}},
	}
	for _, caso := range fora {
		t.Run(caso.nome, func(t *testing.T) {
			if err := validaAparencia(&caso.a); err == nil {
				t.Fatalf("%s foi aceito; esperava recusa", caso.nome)
			}
		})
	}
}

// E o contrário: o cardápio inteiro tem que passar. Este teste é o par do medidor do app
// que confere se a whitelist cobre o cardápio da tela — a divergência entre as duas listas
// já quebrou o save duas vezes, calada, porque o erro da API é um `invalido` genérico.
func TestAparenciaAceitaOCardapioInteiro(t *testing.T) {
	for campo, valores := range cardapioDaAparencia {
		for _, v := range valores {
			a := Aparencia{}
			switch campo {
			case "chao":
				a.Chao = ptr(v)
			case "voz":
				a.Voz = ptr(v)
			case "acao":
				a.Acao = ptr(v)
			case "forma":
				a.Forma = ptr(v)
			case "superficie":
				a.Superficie = ptr(v)
			case "peso":
				a.Peso = ptr(v)
			case "densidade":
				a.Densidade = ptr(v)
			case "movimento":
				a.Movimento = ptr(v)
			case "hierarquia":
				a.Hierarquia = ptr(v)
			case "anel":
				a.Anel = ptr(v)
			case "numero":
				a.Numero = ptr(v)
			case "contraste":
				a.Contraste = ptr(v)
			}
			if err := validaAparencia(&a); err != nil {
				t.Fatalf("%s=%q foi recusado; está no próprio cardápio", campo, v)
			}
		}
	}
	auto := Aparencia{Secundaria: ptr("auto")}
	if err := validaAparencia(&auto); err != nil {
		t.Fatal("segunda cor automática recusada")
	}
	hex := Aparencia{Secundaria: ptr("#22AA55")}
	if err := validaAparencia(&hex); err != nil {
		t.Fatal("segunda cor em hex recusada")
	}
}

// O CHÃO LIVRE. Sete chãos nomeados não são "todas as cores do sistema", que é o que o
// dono vende. O hex livre entra pela mesma porta que a cor da marca já usa: aqui o
// servidor confere só a GRAFIA, porque a janela de claridade em que a escada cabe é conta
// de L* e ela mora num lugar só — `escada()`, em src/theme.ts. Um chão fora da janela não
// é recusado, ele anda para dentro dela dentro do próprio solver, e por isso nenhuma
// aparência que ninguém mediu chega a ser representável.
func TestChaoAceitaHexLivreEOsSeteNomes(t *testing.T) {
	for _, c := range []string{"#0b0a0a", "#123456", "#FFFFFF", "carvao", "linho"} {
		a := Aparencia{Chao: ptr(c)}
		if err := validaAparencia(&a); err != nil {
			t.Fatalf("chão %q recusado", c)
		}
	}
	for _, c := range []string{"#abc", "azul", "rgb(0,0,0)", ""} {
		a := Aparencia{Chao: ptr(c)}
		if err := validaAparencia(&a); err == nil {
			t.Fatalf("chão %q aceito; esperava recusa", c)
		}
	}
}
