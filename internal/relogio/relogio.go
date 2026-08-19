// Package relogio é o ÚNICO relógio do produto.
//
// O LinkGym é um app brasileiro e todo prazo dele é civil, não astronômico: "a ficha de
// hoje", "a mensalidade de agosto", "há 9 dias sem treinar". Antes disto o produto lia a
// hora da máquina — e a máquina é um contêiner Alpine sem tzdata, ou seja UTC. Entre 21h e
// meia-noite no horário de Brasília o app já estava no dia seguinte: a ficha do aluno
// virava às 21h, e no dia 31 às 21h a competência pulava para o mês seguinte, deixando o
// mês certo em aberto sem caminho de quitação.
//
// O Makefile já registrava o sintoma ("entre 21h e meia-noite no horario de Brasilia os
// dois apontam para DIAS DIFERENTES. Custou uma investigacao inteira") e o consertava só
// nos testes, com TZ=UTC. Isto conserta na produção.
package relogio

import (
	"time"
	// O contêiner não tem tzdata instalado. Embutir a base do Go custa ~450 KB no binário
	// e vale exatamente o preço de não depender de o Dockerfile lembrar de instalar nada.
	_ "time/tzdata"
)

// O fuso do produto. Não é configurável de propósito: um app com dois fusos possíveis é um
// app com dois significados possíveis para "hoje".
var Fuso = must("America/Sao_Paulo")

func must(nome string) *time.Location {
	loc, err := time.LoadLocation(nome)
	if err != nil {
		// Impossível com time/tzdata embutido. Se acontecer, UTC silencioso seria pior que
		// o pânico: seria o mesmo bug de volta, sem ninguém saber.
		panic("relogio: fuso " + nome + " indisponível: " + err.Error())
	}
	return loc
}

// Agora é o que todo serviço recebe como `now`. Trocar isto na raiz de composição conserta
// todo `now.Format("2006-01-02")` do repo de uma vez, sem tocar em nenhum serviço.
func Agora() time.Time { return time.Now().In(Fuso) }

// Dia é a data civil de t no fuso do produto. Converte antes de truncar, então funciona
// mesmo quando quem chama injeta um instante em UTC — o que os testes fazem.
func Dia(t time.Time) string { return t.In(Fuso).Format("2006-01-02") }

// Competencia é o mês civil de t, no formato que o schema exige: o dia 1, como date.
// É a chave de mensalidade_pagamentos.month, e o CHECK da tabela recusa qualquer outro dia.
func Competencia(t time.Time) string {
	l := t.In(Fuso)
	return time.Date(l.Year(), l.Month(), 1, 0, 0, 0, 0, Fuso).Format("2006-01-02")
}
