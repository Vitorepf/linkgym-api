# LinkGym v1 — design

Data: 2026-08-18  
Status: ativo — repositórios `linkgym-api` e `linkgym-app` criados  
Recorte: loop diário personal ↔ aluno (24 telas do protótipo v2 + Modernist)

## 1. O que é

LinkGym é o sistema operacional do personal trainer. O v1 existe para ser o app que o personal abre todo dia — e isso só acontece se o aluno também abrir.

Não é um app de treino solto. Não há aluno sem personal. A pessoa só entra se um personal mandar convite.

Referências, com papéis fixos:

- **MFIT** — velocidade do personal (prescrever, publicar, ver a turma).
- **Whoop** — densidade e qualidade da tela do aluno (um número, um botão, telemetria).
- **Duolingo** — motivo para voltar (ofensiva, estreia que não falha, retomada sem culpa).

Design visual: **Modernist no telefone** — fundo `#0b0a0a`, Archivo 800, raio 0, um acento só (a cor do personal). O style guide PULSE (mint/coral/âmbar, Geist, anéis Apple) não é o sistema do produto.

## 2. Fora do v1

Não entram neste recorte: financeiro, loja, nutrição, HealthKit/Watch, web, IA gerando ficha no caminho crítico, cadastro de aluno sem convite, app separado por personal nas lojas.

A fila de jobs no Postgres pode existir vazia (para IA depois). Não há tela de IA no v1.

## 3. Quem usa e como entra

Um aplicativo na loja (iOS e Android). Duas contas: personal e aluno.

Depois da loja, **ninguém vê a marca LinkGym**. O ícone da loja pode dizer LinkGym. Dentro do app, nome, logo e cor são do personal ativo.

**Aluno**

- Não existe “entrar sem personal”.
- Primeiro acesso: convite + telefone + código de 4 dígitos. Sem senha.
- Na prática tem um personal. A conta é da pessoa, não do estúdio.
- Se outro personal convidar: a pessoa troca o estúdio ativo (logo, cor, layout, ficha atual). **Os dados dela permanecem** — cargas, histórico, PRs, o que o corpo já fez. Não se cria uma segunda vida no app. Não se apaga o acervo do personal anterior.
- Dois personais = um aluno, um interruptor de marca, um acervo.

**Personal**

- Opera o próprio estúdio. Vê a marca dele, não LinkGym.
- Convida alunos. Publica treinos. Vê o fio (prescrito vs feito) e a atenção do dia.

## 4. Telas do v1

Fonte: protótipo `LinkGym v2` (Claude Design / Modernist). Isto é o escopo, não um backlog posterior.

### App do personal (8)

1. Painel com ações (1 toque)
2. Atenção do dia (90 s) — poucos alunos, nunca a turma inteira
3. Revisão da semana (em lote)
4. Aluna + ação sugerida
5. De onde partir (toque 1)
6. Ajustar tudo numa tela (toque 2)
7. Publicar, inclusive em lote (toque 3)
8. Retorno da sessão

### App do aluno (16)

1. Convite do personal (D0)
2. Três perguntas (D0)
3. Pronto · o que acontece (D0)
4. Estreia · dia 1 (D1)
5. Compromisso declarado (testemunha)
6. Entrada sem fricção (0 toque). A tela Hoje é o caminho v1. Widget e Live Activity são a mesma intenção; se o Expo não entregar, não atrasam o recorte — entram quando houver módulo nativo.
7. Hoje · prontidão (Whoop)
8. Ficha como leitura
9. Série atual gigante
10. Como fazer · vídeo
11. Descanso + esforço
12. Ofensiva + XP (Duolingo)
13. Recorde pessoal (PR)
14. Ofensiva, liga, selos
15. Perfil que motiva
16. Retomada · dia 11

O sucesso é a retroalimentação: publicar → aluno executa → personal vê retorno → ajusta → atenção aponta quem parou → retomada.

## 5. Domínio

Termos canônicos (não misturar):

| Termo | Significado |
| --- | --- |
| Pessoa | Conta humana (telefone). Pode ser personal, aluno, ou os dois. |
| Estúdio | Marca do personal: nome, logo, cor (`--ac`). White label. |
| Vínculo | Pessoa ↔ estúdio. Ativo = marca e ficha atuais na tela. |
| Modelo | Estrutura do treino (exercícios, ordem, séries previstas). |
| Prescrição | Modelo aplicado a um aluno, com **cargas e detalhes daquela pessoa**. |
| Sessão | Execução de um dia: séries feitas, cargas, descanso, esforço. |
| Fio | Série temporal prescrito vs feito, por aluno, por dia. |
| Ofensiva | Sequência de cumprimentos da meta combinada (não necessariamente 7 dias corridos). |
| Protetor | Primeira falha não zera a ofensiva. Gasta sozinho. |
| Atenção do dia | Fila curta de alunos que precisam de um toque. |

Regras:

- Sem vínculo ativo, o aluno não usa o app.
- Trocar de personal troca o vínculo ativo e o tema. O acervo da pessoa permanece. Liga, selos de estúdio e ficha atual são do personal ativo. Cargas, PRs e histórico de sessões são da pessoa.
- Publicar em lote replica o **modelo**. Nunca replica carga. Cada prescrição nasce das informações daquele aluno (última sessão, progressão, o que ele de fato fez). O personal confirma a estrutura em segundos; o sistema preenche o individual.
- Trocar exercício no meio da sessão (máquina ocupada) avisa o personal e não quebra a ofensiva.
- A sessão grava no celular e sincroniza quando houver rede. Se a sessão não subir, o fio e a atenção do dia estão errados — sincronizar é tão crítico quanto publicar.

## 6. Stack

- **App:** Expo (TypeScript), um binário, duas contas. Swift só depois, e só onde o Expo não chega (HealthKit, Watch, widget nativo). Não há reescrita total em Swift no horizonte do v1.
- **API:** Go.
- **Banco:** Postgres (fonte da verdade, inclusive fila de jobs — River ou equivalente).
- **Arquivos:** object storage (R2 ou S3). Logo, vídeo de exercício, foto de evolução. Postgres guarda o caminho.
- **Redis / Timescale:** não no v1. Entram quando volume exigir.
- **Docker:** Compose no repositório da API (Postgres + MinIO local + API). Expo roda na máquina, não em container. Produção: container Go + Postgres gerenciado + R2/S3 reais.

## 7. Repositórios

Dois:

- `linkgym-api` — Go, migrations, Docker Compose, contrato HTTP (OpenAPI).
- `linkgym-app` — Expo, personal e aluno, EAS / lojas.

A costura é o contrato HTTP. Um campo novo na sessão muda os dois. Não há terceiro repositório de contratos nem de infra neste recorte.

Cópia canônica: `linkgym-api/docs/superpowers/specs/2026-08-18-linkgym-v1-design.md`. O app aponta para o mesmo contrato.

## 8. White label

Depois do login, o app busca o estúdio ativo: nome, logo (URL do storage), cor de acento.

Tokens de produto (fixos): fundo, tinta, divisor, raio 0, Archivo.

Token de cliente (por estúdio): `--ac`, nome, logo.

Não existem paletas A/B/C da plataforma. A paleta é a do personal.

## 9. API — responsabilidade

A API é dona da verdade. O app não inventa ofensiva, atenção do dia nem carga de lote no cliente além do rascunho offline da sessão.

Endpoints lógicos do v1 (não REST literal — o plano de implementação detalha paths):

- Auth: convite, código, sessão.
- Estúdio ativo e troca de vínculo.
- Turma, atenção do dia, revisão da semana.
- Modelo → ajustar → publicar (um ou lote).
- Sessão: upload/sync, retorno.
- Fio, ofensiva, PR, perfil do aluno.
- Mídia: URL assinada para upload.

Push: convite aceito, treino publicado, sessão sincronizada, aluno parado (retomada).

## 10. Falhas que o v1 tem que aguentar

- Academia sem rede: sessão local → sync. Conflito: o servidor ganha em prescrição; o cliente ganha nas séries executadas que ainda não existiam no servidor.
- Convite expirado: personal reenvia. Não há “conta órfã”.
- Lote: se faltar histórico de um aluno, a prescrição usa o último conhecido ou a carga do modelo marcada como “partida” da estreia — nunca a carga de outro aluno.
- Personal com 100 alunos: atenção do dia limitada a uns poucos (o protótipo usa 3). O resto não aparece como tarefa.

## 11. Testes

Antes de feature nova, o loop tem que ter teste que prove:

1. Aluno sem convite não entra.
2. Publicar em lote gera prescrições com cargas diferentes por aluno, derivadas do histórico de cada um.
3. Sessão offline sincroniza e atualiza o fio.
4. Trocar de personal muda tema e ficha ativa e preserva PRs/histórico da pessoa.
5. Primeira falta gasta o protetor; D11 devolve retomada curta, não zera o acervo.

App: testes de fluxo nas telas do loop (convite → hoje → série → feito). API: testes de domínio nos itens acima. Sem isso, o recorte não está fechado.

## 12. Critério de pronto do v1

Um personal convida um aluno no telefone. O aluno entra na marca do personal, faz o treino de hoje, o personal vê o feito e o vão. Um segundo aluno no lote recebe o mesmo modelo com cargas dele. Um aluno que some recebe a retomada. Ninguém vê “LinkGym” depois do login.
