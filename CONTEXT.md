# LinkGym

Sistema operacional do personal trainer. O aluno só existe com um personal; a conta é da pessoa, o nós é do time ativo.

## Language

**Pessoa**:
Conta humana identificada por **telefone**. Pode ser personal, aluno, ou os dois. O acervo do corpo (cargas, PRs, sessões) pertence a ela. E-mail é opcional, não é a chave.
_Avoid_: usuário, conta, aluno-como-identidade, client, device id

**Time**:
Nós em volta de um personal: nome, logo, cor de acento. Na tela só o nome (Fred, Iron Lab) — nunca a palavra Time, nunca LinkGym.
_Avoid_: estúdio, academia, gym, tenant, white-label, workspace, assessoria, casa, equipe, marca

**Vínculo**:
Ligação Pessoa ↔ Time. O vínculo ativo escolhe nome na tela, ficha atual, liga e selos. Trocar de personal troca o vínculo ativo, não a Pessoa.
_Avoid_: membership, matrícula, turma (turma é a lista de alunos daquele time, não o Time)

**Convite**:
Única porta de entrada do aluno: o personal manda, a pessoa entra com telefone e código de 4 dígitos. Sem convite não há aluno.
_Avoid_: signup, cadastro, share link genérico

**Mensalidade**:
Valor combinado e dia do vencimento daquele Vínculo, digitados uma vez pelo personal; ele marca pago ou em aberto, e o app nunca cobra.
_Avoid_: financeiro, caixa como entidade, cobrança, checkout, gateway, PSP, boleto, split, nota fiscal, inadimplente, restringir acesso, plano

Receita recorrente, ticket, ativos e em aberto são LEITURAS da Mensalidade sobre a turma. Não viram termo, não viram tabela, não viram tela própria.

**Modelo**:
Estrutura do treino (exercícios, ordem, séries previstas). Publicar em lote replica o modelo, nunca a carga.
_Avoid_: treino, ficha, template, workout (ambíguos com prescrição e sessão)

**Prescrição**:
Modelo aplicado a uma Pessoa, com cargas e detalhes daquela pessoa. Cada aluno no lote ganha a sua.
_Avoid_: treino publicado, ficha compartilhada, carga do modelo

**Sessão**:
Execução de um dia: séries feitas, cargas, descanso, esforço. Grava no celular e sincroniza. Conflito: servidor ganha na prescrição; cliente ganha nas séries executadas que o servidor ainda não tinha.
_Avoid_: treino feito, workout log, check-in

**Fio**:
Série temporal prescrito vs feito, por aluno, por dia. Não é uma entidade à parte — é a leitura da Prescrição contra a Sessão.
_Avoid_: feed, timeline, dashboard

**Cumprimento**:
O ato mínimo que fecha o dia da Ofensiva: a Sessão prescrita daquele dia fechada — nunca volume, carga ou percentual do prescrito.
_Avoid_: meta batida, meta diária, aderência, compliance, 100% do treino

**Ofensiva**:
Sequência de Cumprimentos no Vínculo com o personal ativo. Vive no Vínculo, não na Pessoa e não no Time inteiro. Não é necessariamente 7 dias corridos.
_Avoid_: streak genérico da conta, hábito, gamificação, ofensiva do time

**Protetor**:
Primeira falha não zera a Ofensiva. Gasta sozinho.
_Avoid_: freeze, pass, streak freeze (nome de outro produto)

**Atenção do dia**:
Fila curta de alunos daquele time que precisam de um toque agora. Nunca a turma inteira. O protótipo cabe em 3.
_Avoid_: inbox, notificações, CRM, lista de alunos

**PR**:
Recorde de carga da Pessoa num exercício. Permanece ao trocar de time.
_Avoid_: recorde do time, ranking da liga

**XP**:
Pontos da Ofensiva no Vínculo ativo. Trocar de personal troca a liga; não leva o XP embora para o outro time.
_Avoid_: pontos da conta, score global

**Prontidão**:
Como a Pessoa está hoje (energia, dor, sono). É do corpo, não do time.
_Avoid_: Whoop, readiness score de wearable

**Retomada**:
Cartão curto quando o aluno some. Vive no Vínculo. Não apaga PR, carga nem histórico.
_Avoid_: win-back, reativação de CRM, reset de conta
