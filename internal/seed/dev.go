package seed

import (
	"context"
	"database/sql"
	"fmt"
)

const (
	PhoneFred  = "+5511900000001"
	PhoneVitor = "+5511900000002"
	PhoneHuan  = "+5511900000003"
	PhoneJose  = "+5511900000004"

	TimeName    = "Fred"
	AccentColor = "#ec3013"
	InviteVitor = "FRED-VITOR"
	InviteHuan  = "FRED-HUAN"
	InviteJose  = "FRED-JOSE"
)

// nunca: ainda não fechou um dia. last_fulfilled_on fica nulo.
const nunca = -1

type person struct {
	phone string
	name  string
	role  string
	code  string
	// pain vai para o onboarding do Vínculo.
	pain bool
	// count é a Ofensiva corrente; idle são os dias desde o último Cumprimento.
	count int
	idle  int
	// reason e rank entram na Atenção do dia. reason vazio fica fora da fila.
	reason string
	rank   int
}

// turma: o resto do time do Fred. Telefone e convite saem do índice, para o
// bloco abaixo ser só nome e situação. Nomes curtos e longos de propósito: a
// tela precisa aguentar os dois.
func turma() []person {
	rest := []person{
		{name: "Ana", count: 21, idle: 0},
		{name: "Rui", idle: 21, reason: "student_stopped", rank: 3},
		{name: "Léo", count: 9, idle: 1},
		{name: "Bia", idle: nunca, reason: "debut", rank: 7},
		{name: "Tiago Nunes", count: 5, idle: 0},
		{name: "Camila Prado", count: 12, idle: 1},
		{name: "Rafael Aguiar", count: 3, idle: 0},
		{name: "Juliana Sampaio", count: 8, idle: 1},
		{name: "Bruno Tavares", count: 2, idle: 0},
		{name: "Larissa Fontes", count: 17, idle: 1},
		{name: "Marcos Vinícius Portela", count: 6, idle: 0},
		{name: "Patrícia Rezende", count: 4, idle: 1},
		{name: "Otávio Bastos", count: 11, idle: 0},
		{name: "Sabrina Queiroz", idle: 3, pain: true, reason: "pain_flag", rank: 5},
		{name: "Gustavo Peçanha", count: 7, idle: 1},
		{name: "Maria Aparecida da Silva Nascimento", idle: 12, reason: "student_stopped", rank: 4},
		{name: "João Vitor de Albuquerque Fontenele", idle: 2, pain: true, reason: "pain_flag", rank: 6},
		{name: "Rosângela Bittencourt de Menezes", count: 25, idle: 0},
		{name: "Wellington dos Santos Cavalcanti", count: 30, idle: 1, reason: "high_effort", rank: 9},
		{name: "Fernanda Kuroda", count: 13, idle: 1},
		{name: "Diego Sarmento", idle: 6, reason: "student_stopped", rank: 2},
		{name: "Tatiane Marques", count: 10, idle: 0},
		{name: "Igor Bandeira", count: 1, idle: 1},
		{name: "Cristiane Vasconcelos Palmeira", idle: nunca, reason: "debut", rank: 8},
		{name: "Heitor Assunção", count: 16, idle: 0},
	}
	for i := range rest {
		rest[i].role = "student"
		rest[i].phone = fmt.Sprintf("+55119000000%02d", i+5)
		rest[i].code = fmt.Sprintf("FRED-%02d", i+5)
	}
	return rest
}

func Dev(ctx context.Context, db *sql.DB) error {
	people := []person{
		{phone: PhoneFred, name: "Fred", role: "owner", idle: nunca},
		{phone: PhoneVitor, name: "Vitor", role: "student", code: InviteVitor, idle: nunca},
		{phone: PhoneHuan, name: "Huan", role: "student", code: InviteHuan, idle: nunca},
		{phone: PhoneJose, name: "Jose", role: "student", code: InviteJose, idle: nunca, reason: "student_stopped", rank: 1},
	}
	people = append(people, turma()...)

	for _, p := range people {
		if _, err := db.ExecContext(ctx, `
			INSERT INTO people (phone, name)
			VALUES ($1, $2)
			ON CONFLICT (phone) DO UPDATE SET name = EXCLUDED.name, updated_at = now()`,
			p.phone, p.name,
		); err != nil {
			return fmt.Errorf("person %s: %w", p.name, err)
		}
	}

	if _, err := db.ExecContext(ctx, `
		INSERT INTO studios (owner_person_id, name, accent_color)
		SELECT id, $1, $2 FROM people WHERE phone = $3
		ON CONFLICT (owner_person_id) DO UPDATE
		SET name = EXCLUDED.name, accent_color = EXCLUDED.accent_color, updated_at = now()`,
		TimeName, AccentColor, PhoneFred,
	); err != nil {
		return fmt.Errorf("time: %w", err)
	}

	if _, err := db.ExecContext(ctx, `
		INSERT INTO bonds (person_id, studio_id, role, status)
		SELECT p.id, s.id, 'owner', 'active'
		FROM people p
		JOIN studios s ON s.owner_person_id = p.id
		WHERE p.phone = $1
		ON CONFLICT (person_id, studio_id) DO UPDATE
		SET role = 'owner', status = 'active', updated_at = now()`,
		PhoneFred,
	); err != nil {
		return fmt.Errorf("owner bond: %w", err)
	}

	for _, p := range people {
		if p.role != "student" {
			continue
		}
		onboarding := fmt.Sprintf(`{"experience":"training","days_per_week":3,"pain":%t}`, p.pain)

		if _, err := db.ExecContext(ctx, `
			INSERT INTO bonds (person_id, studio_id, role, status, onboarding)
			SELECT p.id, s.id, 'student', 'active', $3::jsonb
			FROM people p
			JOIN people owner ON owner.phone = $1
			JOIN studios s ON s.owner_person_id = owner.id
			WHERE p.phone = $2
			ON CONFLICT (person_id, studio_id) DO UPDATE
			SET role = 'student', status = 'active',
			    onboarding = EXCLUDED.onboarding,
			    updated_at = now()`,
			PhoneFred, p.phone, onboarding,
		); err != nil {
			return fmt.Errorf("student bond %s: %w", p.name, err)
		}

		if _, err := db.ExecContext(ctx, `
			INSERT INTO invites (
				studio_id, created_by_person_id, phone, code,
				expires_at, accepted_at, accepted_person_id
			)
			SELECT s.id, owner.id, p.phone, $1, now() + interval '10 years', now(), p.id
			FROM people p
			JOIN people owner ON owner.phone = $2
			JOIN studios s ON s.owner_person_id = owner.id
			WHERE p.phone = $3
			ON CONFLICT (code) DO UPDATE
			SET accepted_at = COALESCE(invites.accepted_at, now()),
			    accepted_person_id = EXCLUDED.accepted_person_id`,
			p.code, PhoneFred, p.phone,
		); err != nil {
			return fmt.Errorf("invite %s: %w", p.name, err)
		}

		if _, err := db.ExecContext(ctx, `
			INSERT INTO streaks (bond_id, current_count, protector_available, last_fulfilled_on)
			SELECT b.id, $3, true, CASE WHEN $4::int >= 0 THEN current_date - $4::int END
			FROM bonds b
			JOIN people p ON p.id = b.person_id
			JOIN studios s ON s.id = b.studio_id
			JOIN people owner ON owner.id = s.owner_person_id
			WHERE p.phone = $1 AND owner.phone = $2
			ON CONFLICT (bond_id) DO NOTHING`,
			p.phone, PhoneFred, p.count, p.idle,
		); err != nil {
			return fmt.Errorf("ofensiva %s: %w", p.name, err)
		}
	}

	if _, err := db.ExecContext(ctx, `
		UPDATE people p
		SET active_bond_id = b.id, updated_at = now()
		FROM bonds b
		JOIN studios s ON s.id = b.studio_id
		JOIN people owner ON owner.id = s.owner_person_id
		WHERE b.person_id = p.id
		  AND owner.phone = $1`,
		PhoneFred,
	); err != nil {
		return fmt.Errorf("active bond: %w", err)
	}

	for _, p := range people {
		if p.reason == "" {
			continue
		}
		if _, err := db.ExecContext(ctx, `
			INSERT INTO attention_items (studio_id, person_id, for_date, reason, rank)
			SELECT s.id, p.id, current_date, $3, $4
			FROM people p
			JOIN people owner ON owner.phone = $1
			JOIN studios s ON s.owner_person_id = owner.id
			WHERE p.phone = $2
			ON CONFLICT (studio_id, person_id, for_date) DO UPDATE
			-- applied_at volta a NULL: o seed promete uma FILA, e uma linha ja aplicada de
			-- uma rodada anterior deixava o item fora dela em silencio.
			SET reason = EXCLUDED.reason, rank = EXCLUDED.rank, applied_at = NULL`,
			PhoneFred, p.phone, p.reason, p.rank,
		); err != nil {
			return fmt.Errorf("attention %s: %w", p.name, err)
		}
	}

	if err := seedWorkout(ctx, db); err != nil {
		return err
	}

	if err := seedMensalidades(ctx, db); err != nil {
		return err
	}

	// `make test` re-semeia e fecha sessões NO MESMO banco, e cada rodada deixava os
	// alertas para trás: o Painel do Fred chegou a dizer "Retornos por ler 452". Semear é
	// restaurar o dia dev — os alertas acumulados saem junto com o resto do lixo.
	if _, err := db.ExecContext(ctx, `
		DELETE FROM session_alerts sa
		USING studios s, people o
		WHERE sa.studio_id = s.id AND o.id = s.owner_person_id AND o.phone = $1`,
		PhoneFred,
	); err != nil {
		return fmt.Errorf("limpar alertas: %w", err)
	}

	return nil
}

// A MENSALIDADE de cada Vínculo: valor e vencimento variados mas determinísticos (saem do
// hash do telefone, então re-semear não embaralha). O mês corrente nasce PAGO para a
// maioria; ficam em aberto o Jose e os que o hash sorteia — a tela de operação precisa de
// nomes reais dos dois lados.
func seedMensalidades(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `
		INSERT INTO mensalidades (bond_id, amount_cents, due_day)
		SELECT b.id,
		       30000 + (abs(hashtext(p.phone)) % 4) * 5000,
		       5 + (abs(hashtext(p.phone)) % 4) * 5
		FROM bonds b
		JOIN people p ON p.id = b.person_id
		JOIN studios s ON s.id = b.studio_id
		JOIN people owner ON owner.id = s.owner_person_id
		WHERE owner.phone = $1 AND b.role = 'student' AND b.status = 'active'
		ON CONFLICT (bond_id) DO UPDATE
		SET amount_cents = EXCLUDED.amount_cents, due_day = EXCLUDED.due_day, updated_at = now()`,
		PhoneFred,
	); err != nil {
		return fmt.Errorf("mensalidades: %w", err)
	}

	if _, err := db.ExecContext(ctx, `
		INSERT INTO mensalidade_pagamentos (bond_id, month)
		SELECT b.id, date_trunc('month', current_date)::date
		FROM bonds b
		JOIN people p ON p.id = b.person_id
		JOIN studios s ON s.id = b.studio_id
		JOIN people owner ON owner.id = s.owner_person_id
		WHERE owner.phone = $1 AND b.role = 'student' AND b.status = 'active'
		  AND p.phone <> $2
		  AND abs(hashtext(p.phone)) % 4 <> 1
		ON CONFLICT (bond_id, month) DO NOTHING`,
		PhoneFred, PhoneJose,
	); err != nil {
		return fmt.Errorf("pagamentos: %w", err)
	}

	// Semear é RESTAURAR: um toque em "Pago" (no app ou num teste) persiste, e a promessa
	// do seed — Jose em aberto — viraria mentira na segunda rodada. O pagamento do mês
	// dele sai junto com o resto do estado restaurado.
	if _, err := db.ExecContext(ctx, `
		DELETE FROM mensalidade_pagamentos pg
		USING bonds b, people p
		WHERE pg.bond_id = b.id AND b.person_id = p.id
		  AND p.phone = $1
		  AND pg.month = date_trunc('month', current_date)::date`,
		PhoneJose,
	); err != nil {
		return fmt.Errorf("restaurar jose: %w", err)
	}
	return nil
}

func seedWorkout(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `
		INSERT INTO exercises (studio_id, name)
		SELECT s.id, e.name
		FROM studios s
		JOIN people owner ON owner.id = s.owner_person_id
		-- Biblioteca de verdade. Tres exercicios nao sao uma biblioteca: com tres, nao da
		-- para montar uma estrutura de treino, nao da para trocar um por uma variacao, e
		-- qualquer tela de escolha parece vazia. Supino, Remada e Agachamento continuam
		-- aqui e com o mesmo nome porque o Modelo Treino A e varios testes os procuram por
		-- nome (internal/migrate/schema_test.go, internal/workout/service_test.go).
		--
		-- Os nomes carregam o EQUIPAMENTO, que e o que separa uma variacao da outra no
		-- jeito como o personal fala: "Supino" e "Supino com halteres" sao o mesmo padrao
		-- motor com carga e material diferentes.
		CROSS JOIN (VALUES
			('Supino'), ('Supino inclinado'), ('Supino com halteres'), ('Crucifixo'),
			('Remada'), ('Remada curvada'), ('Remada unilateral'), ('Puxada alta'),
			('Barra fixa'), ('Pulldown'),
			('Agachamento'), ('Agachamento livre'), ('Agachamento frontal'), ('Leg press'),
			('Cadeira extensora'), ('Mesa flexora'), ('Afundo'), ('Passada'),
			('Levantamento terra'), ('Terra romeno'), ('Stiff'),
			('Desenvolvimento militar'), ('Desenvolvimento com halteres'),
			('Elevacao lateral'), ('Elevacao frontal'), ('Crucifixo inverso'),
			('Rosca direta'), ('Rosca martelo'), ('Rosca scott'),
			('Triceps testa'), ('Triceps corda'), ('Mergulho'),
			('Panturrilha em pe'), ('Panturrilha sentado'),
			('Abdominal'), ('Prancha'), ('Elevacao de pernas')
		) AS e(name)
		WHERE owner.phone = $1
		ON CONFLICT (studio_id, name) DO NOTHING`,
		PhoneFred,
	); err != nil {
		return fmt.Errorf("exercises: %w", err)
	}

	if _, err := db.ExecContext(ctx, `
		INSERT INTO models (studio_id, name, created_by_person_id)
		SELECT s.id, 'Treino A', owner.id
		FROM studios s
		JOIN people owner ON owner.id = s.owner_person_id
		WHERE owner.phone = $1
		-- O indice de nome do Modelo e PARCIAL desde a migration 00004 (so vale para o
		-- que esta na biblioteca), e ON CONFLICT em indice parcial exige repetir o
		-- predicado.
		ON CONFLICT (studio_id, name) WHERE archived_at IS NULL DO NOTHING`,
		PhoneFred,
	); err != nil {
		return fmt.Errorf("model: %w", err)
	}

	if _, err := db.ExecContext(ctx, `
		INSERT INTO model_items (model_id, exercise_id, position, planned_sets, planned_reps, starter_load_kg)
		SELECT m.id, ex.id, x.position, 3, '8-12', x.starter
		FROM models m
		JOIN studios s ON s.id = m.studio_id
		JOIN people owner ON owner.id = s.owner_person_id
		CROSS JOIN (VALUES
			(1, 'Supino', 20::numeric),
			(2, 'Remada', 18::numeric),
			(3, 'Agachamento', 40::numeric)
		) AS x(position, name, starter)
		JOIN exercises ex ON ex.studio_id = s.id AND ex.name = x.name
		WHERE owner.phone = $1 AND m.name = 'Treino A'
		ON CONFLICT (model_id, position) DO UPDATE
		SET exercise_id = EXCLUDED.exercise_id,
		    starter_load_kg = EXCLUDED.starter_load_kg`,
		PhoneFred,
	); err != nil {
		return fmt.Errorf("model items: %w", err)
	}

	loads := []struct {
		phone  string
		load   float64
		source string
	}{
		{PhoneVitor, 40, "history"},
		{PhoneHuan, 22.5, "history"},
		{PhoneJose, 20, "starter"},
	}

	for _, row := range loads {
		var prescriptionID string
		err := db.QueryRowContext(ctx, `
			INSERT INTO prescriptions (studio_id, person_id, model_id, for_date, status, published_at)
			SELECT s.id, p.id, m.id, current_date, 'published', now()
			FROM people p
			JOIN people owner ON owner.phone = $1
			JOIN studios s ON s.owner_person_id = owner.id
			JOIN models m ON m.studio_id = s.id AND m.name = 'Treino A'
			WHERE p.phone = $2
			ON CONFLICT (person_id, studio_id, for_date) WHERE status = 'published'
			DO UPDATE SET updated_at = now()
			RETURNING id`,
			PhoneFred, row.phone,
		).Scan(&prescriptionID)
		if err != nil {
			return fmt.Errorf("prescription %s: %w", row.phone, err)
		}

		if _, err := db.ExecContext(ctx, `
			INSERT INTO prescription_items (
				prescription_id, exercise_id, position, planned_sets, planned_reps, load_kg, load_source
			)
			SELECT $1, mi.exercise_id, mi.position, mi.planned_sets, mi.planned_reps,
			       CASE WHEN mi.position = 1 THEN $2 ELSE mi.starter_load_kg END,
			       -- A fonte e POR ITEM. So a posicao 1 recebe a carga daquele corpo; as
			       -- outras vem de mi.starter_load_kg, que e o chute generico do Modelo e
			       -- e igual para todo mundo. Carimbar a linha inteira com $3 fazia o seed
			       -- declarar 'history' em exercicio que ninguem nunca executou — e a
			       -- cascata propagava essa mentira para toda ficha seguinte.
			       CASE WHEN mi.position = 1 THEN $3 ELSE 'starter' END
			FROM model_items mi
			JOIN models m ON m.id = mi.model_id
			WHERE m.id = (SELECT model_id FROM prescriptions WHERE id = $1)
			ON CONFLICT (prescription_id, position) DO UPDATE
			SET load_kg = EXCLUDED.load_kg, load_source = EXCLUDED.load_source`,
			prescriptionID, row.load, row.source,
		); err != nil {
			return fmt.Errorf("prescription items %s: %w", row.phone, err)
		}
	}

	return nil
}
