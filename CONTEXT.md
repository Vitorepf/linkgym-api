# LinkGym

Sistema operacional do personal trainer. O aluno só existe com um personal; a conta é da pessoa, a marca é do estúdio ativo.

## Language

**Pessoa**:
Conta humana identificada por telefone. Pode ser personal, aluno, ou os dois. O acervo do corpo (cargas, PRs, sessões) pertence a ela.
_Avoid_: usuário, conta, aluno-como-identidade, client

**Estúdio**:
Marca de um personal: nome, logo, cor de acento. Depois do login, é isto que aparece na tela — não LinkGym.
_Avoid_: academia, gym, tenant, white-label, workspace

**Vínculo**:
Ligação Pessoa ↔ Estúdio. O vínculo ativo escolhe marca, ficha atual, liga e selos. Trocar de personal troca o vínculo ativo, não a Pessoa.
_Avoid_: membership, matrícula, turma (turma é o conjunto de vínculos de aluno de um estúdio)

**Convite**:
Única porta de entrada do aluno: o personal manda, a pessoa entra com telefone e código de 4 dígitos. Sem convite não há aluno.
_Avoid_: signup, cadastro, share link genérico

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

**Ofensiva**:
Sequência de cumprimentos da meta combinada com o personal ativo. Vive no Vínculo, não na Pessoa. Não é necessariamente 7 dias corridos.
_Avoid_: streak genérico da conta, hábito, gamificação

**Protetor**:
Primeira falha não zera a Ofensiva. Gasta sozinho.
_Avoid_: freeze, pass, streak freeze (nome de outro produto)

**Atenção do dia**:
Fila curta de alunos daquele estúdio que precisam de um toque agora. Nunca a turma inteira. O protótipo cabe em 3.
_Avoid_: inbox, notificações, CRM, lista de alunos

**PR**:
Recorde de carga da Pessoa num exercício. Permanece ao trocar de estúdio.
_Avoid_: recorde do estúdio, ranking da liga
