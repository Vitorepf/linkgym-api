package publish

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/Vitorepf/linkgym-api/internal/voz"
)

type Service struct {
	db  *sql.DB
	now func() time.Time
}

func New(db *sql.DB, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{db: db, now: now}
}

type Model struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Exercise e um item da biblioteca do Time. Nao carrega carga: carga e da Prescricao, que
// e o Modelo aplicado a uma Pessoa. Misturar os dois e o que faria a carga de um aluno
// vazar para outro.
type Exercise struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	HasVideo bool   `json:"has_video"`
}

type ModelDetail struct {
	ID    string      `json:"id"`
	Name  string      `json:"name"`
	Items []ModelItem `json:"items"`
}

type ModelItem struct {
	ID            string  `json:"id"`
	ExerciseID    string  `json:"exercise_id"`
	Name          string  `json:"name"`
	Position      int     `json:"position"`
	PlannedSets   int     `json:"planned_sets"`
	PlannedReps   string  `json:"planned_reps"`
	StarterLoadKg float64 `json:"starter_load_kg"`
}

// NewModel e o que o personal manda para criar uma ESTRUTURA. Repare no que NAO tem aqui:
// carga do aluno. Modelo e "estrutura do treino (exercicios, ordem, series previstas)" e o
// verbete diz que publicar em lote replica o modelo, NUNCA a carga. StarterLoadKg e a
// unica carga que cabe, e e opcional por desenho — a migration diz "so para estreia sem
// historico", e quem tem historico nunca a ve.
type NewModel struct {
	Name  string         `json:"name"`
	Items []NewModelItem `json:"items"`
}

type NewModelItem struct {
	ExerciseID    string   `json:"exercise_id"`
	PlannedSets   int      `json:"planned_sets"`
	PlannedReps   string   `json:"planned_reps"`
	RestSeconds   *int     `json:"rest_seconds"`
	Notes         *string  `json:"notes"`
	StarterLoadKg *float64 `json:"starter_load_kg"`
}

type Draft struct {
	DraftID string      `json:"draft_id"`
	Items   []DraftItem `json:"items"`
}

type DraftItem struct {
	ID          string  `json:"id"`
	ExerciseID  string  `json:"exercise_id"`
	Name        string  `json:"name"`
	PlannedSets int     `json:"planned_sets"`
	PlannedReps string  `json:"planned_reps"`
	LoadKg      float64 `json:"load_kg"`
	LoadSource  string  `json:"load_source"`
	// A frase do personal neste exercício. Ausente é ausente: a tela do aluno não
	// inventa voz no lugar dele.
	Notes *string `json:"notes"`
}

type ItemPatch struct {
	LoadKg      float64 `json:"load_kg"`
	PlannedSets int     `json:"planned_sets"`
	PlannedReps string  `json:"planned_reps"`
	// Opcional de propósito: cliente antigo manda só carga/séries/reps e a frase
	// que já estava no item fica. String vazia apaga.
	Notes *string `json:"notes"`
}

// ItensComCargaDoCorpo replica a ESTRUTURA do Modelo e resolve a CARGA de cada corpo.
//
// Uma expressao so, usada pelos DOIS caminhos de publicacao — o de um aluno
// (DraftFromLast) e o do lote (publishFromModelo). Eles tinham cada um a sua copia, e as
// copias estavam em ordem OPOSTA: um punha a serie executada na frente, o outro punha a
// ficha anterior. A mesma pessoa recebia 40 publicando sozinha e 47,5 entrando no lote.
// Numero que muda conforme o botao que o personal apertou nao e numero, e ninguem confia
// nele. Agora nao ha como divergir: existe uma fonte.
//
// A ORDEM, e o porque de cada degrau:
//  1. workout_sets  — o que o CORPO levantou por ultimo. Ganha de tudo, porque e o unico
//     degrau que e fato: os outros sao intencao. E a tese do produto — depois que o aluno
//     esta treinando, a carga sai dele, nao da cabeca do personal.
//  2. prescription_items da ultima ficha publicada — a intencao mais recente do personal,
//     para exercicio que ele prescreveu mas o corpo ainda nao executou.
//  3. model_items.starter_load_kg — o chute inicial do Modelo, so para estreia sem
//     historico, como diz o comentario da migration.
//  4. zero — e a tela tem que mostrar isso como ausencia, nunca como numero valido.
//
// load_source carimba CADA degrau com o nome dele, e o degrau 2 tem o seu proprio desde a
// migration 00005: 'prescription'. Antes ele herdava ficha.load_source, entao um numero que
// so existia porque o personal o prescreveu voltava carimbado 'history', e a tela dizia "e a
// carga da ultima serie que ele fez" sobre exercicio que aquele corpo nunca executou. Fato
// (o corpo) e intencao (a ficha) precisam de nomes diferentes — senao a coluna nao faz a
// unica coisa que ela existe para fazer, que e deixar o personal ver DE ONDE veio o numero
// em vez de ter que confiar.
//
// Parametros, na ordem: $1 prescriptionID, $2 personID, $3 studioID, $4 forDate, $5 modelID.
const ItensComCargaDoCorpo = `
	INSERT INTO prescription_items (
		prescription_id, exercise_id, position, planned_sets, planned_reps,
		load_kg, rest_seconds, notes, load_source
	)
	SELECT $1, mi.exercise_id, mi.position, mi.planned_sets, mi.planned_reps,
	       COALESCE(corpo.load_kg, ficha.load_kg, mi.starter_load_kg, 0),
	       mi.rest_seconds, mi.notes,
	       CASE
	           WHEN corpo.load_kg IS NOT NULL THEN 'history'
	           WHEN ficha.load_kg IS NOT NULL THEN 'prescription'
	           ELSE 'starter'
	       END
	FROM model_items mi
	LEFT JOIN LATERAL (
		SELECT ws.load_kg
		FROM workout_sets ws
		JOIN workout_sessions sess ON sess.id = ws.session_id
		WHERE sess.person_id = $2 AND sess.studio_id = $3
		  AND sess.finished_at IS NOT NULL
		  AND ws.exercise_id = mi.exercise_id AND ws.load_kg IS NOT NULL
		ORDER BY ws.performed_at DESC
		LIMIT 1
	) corpo ON true
	LEFT JOIN LATERAL (
		SELECT pi.load_kg
		FROM prescriptions pr
		JOIN prescription_items pi
		  ON pi.prescription_id = pr.id AND pi.exercise_id = mi.exercise_id
		WHERE pr.person_id = $2 AND pr.studio_id = $3
		  AND pr.status = 'published' AND pr.for_date <> $4::date
		ORDER BY pr.for_date DESC
		LIMIT 1
	) ficha ON true
	WHERE mi.model_id = $5::uuid`

func (s *Service) ownerTime(ctx context.Context, personID string) (string, error) {
	var role, studioID string
	err := s.db.QueryRowContext(ctx, `
		SELECT b.role, b.studio_id::text
		FROM people p
		JOIN bonds b ON b.id = p.active_bond_id
		WHERE p.id = $1`,
		personID,
	).Scan(&role, &studioID)
	if err == sql.ErrNoRows {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	if role != "owner" {
		return "", ErrForbidden
	}
	return studioID, nil
}

func (s *Service) ListModels(ctx context.Context, ownerID string) ([]Model, error) {
	studioID, err := s.ownerTime(ctx, ownerID)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id::text, name FROM models
		WHERE studio_id = $1 AND archived_at IS NULL
		ORDER BY name`,
		studioID,
	)
	if err != nil {
		return nil, fmt.Errorf("publish models: %w", err)
	}
	defer rows.Close()

	items := []Model{}
	for rows.Next() {
		var m Model
		if err := rows.Scan(&m.ID, &m.Name); err != nil {
			return nil, fmt.Errorf("publish models scan: %w", err)
		}
		items = append(items, m)
	}
	return items, rows.Err()
}

// ListExercises devolve a biblioteca do Time do personal. Sem ela nao existe montar
// estrutura de treino: nao havia rota nenhuma para o personal ver o que ele pode escolher.
func (s *Service) ListExercises(ctx context.Context, ownerID string) ([]Exercise, error) {
	studioID, err := s.ownerTime(ctx, ownerID)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id::text, name, video_object_key IS NOT NULL AND video_object_key <> ''
		FROM exercises
		WHERE studio_id = $1
		ORDER BY name`,
		studioID,
	)
	if err != nil {
		return nil, fmt.Errorf("publish exercises: %w", err)
	}
	defer rows.Close()

	items := []Exercise{}
	for rows.Next() {
		var e Exercise
		if err := rows.Scan(&e.ID, &e.Name, &e.HasVideo); err != nil {
			return nil, fmt.Errorf("publish exercises scan: %w", err)
		}
		items = append(items, e)
	}
	return items, rows.Err()
}

// CreateModel monta uma estrutura nova para o Time do personal.
//
// Nao existia: INSERT INTO models so acontecia no seed, entao o personal nao tinha como
// criar treino nenhum pelo app — a coisa que o dono do produto chama de mais importante.
func (s *Service) CreateModel(ctx context.Context, ownerID string, in NewModel) (*ModelDetail, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, ErrInvalid
	}
	if err := validarItens(in.Items); err != nil {
		return nil, err
	}

	studioID, err := s.ownerTime(ctx, ownerID)
	if err != nil {
		return nil, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	// O nome repetido e recusado pelo indice models_studio_name, que so conta o que esta
	// na biblioteca: ON CONFLICT DO NOTHING transforma a colisao em "nenhuma linha", sem
	// corrida entre um SELECT e o INSERT.
	var modelID string
	err = tx.QueryRowContext(ctx, `
		INSERT INTO models (studio_id, name, created_by_person_id)
		VALUES ($1, $2, $3)
		ON CONFLICT (studio_id, name) WHERE archived_at IS NULL DO NOTHING
		RETURNING id::text`,
		studioID, name, ownerID,
	).Scan(&modelID)
	if err == sql.ErrNoRows {
		return nil, ErrInvalid
	}
	if err != nil {
		return nil, fmt.Errorf("publish create model: %w", err)
	}

	if err := inserirItens(ctx, tx, studioID, modelID, in.Items); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetModel(ctx, ownerID, modelID)
}

// ReplaceModelItems troca a lista INTEIRA do Modelo.
//
// Apagar e regravar em vez de remendar item a item: model_items_position e UNIQUE
// (model_id, position), entao trocar dois exercicios de lugar com UPDATE bateria na
// restricao no meio do caminho. Dentro da mesma transacao, apagar tudo antes de gravar de
// novo nao tem esse problema, e a lista final e exatamente a que chegou.
//
// Nenhuma ficha ja publicada muda por causa disto: prescription_items sao COPIAS, nao
// apontam para model_items. Quem ja recebeu continua com o que recebeu.
func (s *Service) ReplaceModelItems(ctx context.Context, ownerID, modelID string, items []NewModelItem) (*ModelDetail, error) {
	if modelID == "" {
		return nil, ErrNotFound
	}
	if err := validarItens(items); err != nil {
		return nil, err
	}
	studioID, err := s.ownerTime(ctx, ownerID)
	if err != nil {
		return nil, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var doTime int
	if err := tx.QueryRowContext(ctx, `
		SELECT count(*) FROM models
		WHERE id = $1 AND studio_id = $2 AND archived_at IS NULL`,
		modelID, studioID,
	).Scan(&doTime); err != nil {
		return nil, fmt.Errorf("publish model items time: %w", err)
	}
	if doTime == 0 {
		return nil, ErrNotFound
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM model_items WHERE model_id = $1`, modelID); err != nil {
		return nil, fmt.Errorf("publish model items limpa: %w", err)
	}
	if err := inserirItens(ctx, tx, studioID, modelID, items); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE models SET updated_at = now() WHERE id = $1`, modelID); err != nil {
		return nil, fmt.Errorf("publish model items touch: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetModel(ctx, ownerID, modelID)
}

// RenameModel troca so o nome. A estrutura fica onde esta, e as fichas ja publicadas nem
// sabem que isso aconteceu.
func (s *Service) RenameModel(ctx context.Context, ownerID, modelID, name string) error {
	name = strings.TrimSpace(name)
	if modelID == "" {
		return ErrNotFound
	}
	if name == "" {
		return ErrInvalid
	}
	studioID, err := s.ownerTime(ctx, ownerID)
	if err != nil {
		return err
	}

	var ocupado int
	if err := s.db.QueryRowContext(ctx, `
		SELECT count(*) FROM models
		WHERE studio_id = $1 AND name = $2 AND id <> $3 AND archived_at IS NULL`,
		studioID, name, modelID,
	).Scan(&ocupado); err != nil {
		return fmt.Errorf("publish rename model dup: %w", err)
	}
	if ocupado > 0 {
		return ErrInvalid
	}

	res, err := s.db.ExecContext(ctx, `
		UPDATE models SET name = $3, updated_at = now()
		WHERE id = $1 AND studio_id = $2 AND archived_at IS NULL`,
		modelID, studioID, name,
	)
	if err != nil {
		return fmt.Errorf("publish rename model: %w", err)
	}
	return umaLinha(res)
}

// ArchiveModel tira o Modelo da biblioteca sem apagar a linha, e essa e a unica saida
// honesta.
//
// prescriptions.model_id e ON DELETE RESTRICT (migrations/00001_init.sql): cada Prescricao
// e o Modelo aplicado a UMA pessoa, com as cargas do corpo dela. Um DELETE de verdade ou
// seria recusado pelo banco assim que o Modelo tivesse sido publicado uma vez, ou — se
// alguem trocasse a regra por CASCADE — levaria junto a ficha e o historico de quem
// treinou nela. Nenhum dos dois e o que o personal pediu quando apertou apagar: ele quis
// tirar da lista de escolha, nao mexer no passado de ninguem.
//
// Some da biblioteca, some do GET, nao serve mais para montar ficha nova, e o nome volta a
// ficar livre. Quem ja treinou nao percebe nada.
func (s *Service) ArchiveModel(ctx context.Context, ownerID, modelID string) error {
	if modelID == "" {
		return ErrNotFound
	}
	studioID, err := s.ownerTime(ctx, ownerID)
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, `
		UPDATE models SET archived_at = now(), updated_at = now()
		WHERE id = $1 AND studio_id = $2 AND archived_at IS NULL`,
		modelID, studioID,
	)
	if err != nil {
		return fmt.Errorf("publish archive model: %w", err)
	}
	return umaLinha(res)
}

// CreateExercise poe um exercicio novo na biblioteca do Time.
//
// Ate aqui exercicio so nascia no seed, para o Time do Fred: um personal novo abria a
// biblioteca vazia e nao tinha por onde comecar. O nome repetido no mesmo Time e recusado
// pelo UNIQUE (studio_id, name) que ja existe desde a migration 00002 — de novo por
// ON CONFLICT, para nao existir a janela entre o SELECT e o INSERT.
func (s *Service) CreateExercise(ctx context.Context, ownerID, name string) (*Exercise, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrInvalid
	}
	studioID, err := s.ownerTime(ctx, ownerID)
	if err != nil {
		return nil, err
	}
	var id string
	err = s.db.QueryRowContext(ctx, `
		INSERT INTO exercises (studio_id, name) VALUES ($1, $2)
		ON CONFLICT (studio_id, name) DO NOTHING
		RETURNING id::text`,
		studioID, name,
	).Scan(&id)
	if err == sql.ErrNoRows {
		return nil, ErrInvalid
	}
	if err != nil {
		return nil, fmt.Errorf("publish create exercise: %w", err)
	}
	return &Exercise{ID: id, Name: name}, nil
}

// RenameExercise conserta o nome de um exercicio do proprio Time.
//
// O id sozinho nao basta: sem o studio_id no WHERE, mandar o id de outro Time renomearia a
// biblioteca alheia.
func (s *Service) RenameExercise(ctx context.Context, ownerID, exerciseID, name string) error {
	name = strings.TrimSpace(name)
	if exerciseID == "" {
		return ErrNotFound
	}
	if name == "" {
		return ErrInvalid
	}
	studioID, err := s.ownerTime(ctx, ownerID)
	if err != nil {
		return err
	}

	var ocupado int
	if err := s.db.QueryRowContext(ctx, `
		SELECT count(*) FROM exercises
		WHERE studio_id = $1 AND name = $2 AND id <> $3::uuid`,
		studioID, name, exerciseID,
	).Scan(&ocupado); err != nil {
		return fmt.Errorf("publish rename exercise dup: %w", err)
	}
	if ocupado > 0 {
		return ErrInvalid
	}

	res, err := s.db.ExecContext(ctx, `
		UPDATE exercises SET name = $3, updated_at = now()
		WHERE id = $1::uuid AND studio_id = $2`,
		exerciseID, studioID, name,
	)
	if err != nil {
		return fmt.Errorf("publish rename exercise: %w", err)
	}
	return umaLinha(res)
}

func umaLinha(res sql.Result) error {
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// validarItens recusa o que nao e estrutura: lista vazia, item sem exercicio, serie que
// nao existe, e o mesmo exercicio duas vezes — que e quase sempre engano de quem montou, e
// vira duas linhas identicas na ficha do aluno.
func validarItens(items []NewModelItem) error {
	if len(items) == 0 {
		return ErrInvalid
	}
	vistos := map[string]bool{}
	for _, it := range items {
		if it.ExerciseID == "" || it.PlannedSets <= 0 || strings.TrimSpace(it.PlannedReps) == "" {
			return ErrInvalid
		}
		if vistos[it.ExerciseID] {
			return ErrInvalid
		}
		vistos[it.ExerciseID] = true
	}
	return nil
}

// inserirItens grava a lista na ORDEM em que ela chegou: a posicao e o indice do array, e
// nao um campo que o personal digita — numerar exercicio a mao e trabalho que o indice do
// array ja faz.
func inserirItens(ctx context.Context, tx *sql.Tx, studioID, modelID string, items []NewModelItem) error {
	for i, it := range items {
		// O exercicio tem que ser do MESMO Time. Sem esta checagem o personal montaria
		// estrutura com exercicio de outro Time so mandando o id.
		var pertence int
		if err := tx.QueryRowContext(ctx, `
			SELECT count(*) FROM exercises WHERE id = $1::uuid AND studio_id = $2`,
			it.ExerciseID, studioID,
		).Scan(&pertence); err != nil {
			return fmt.Errorf("publish model item exercise: %w", err)
		}
		if pertence == 0 {
			return ErrNotFound
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO model_items (
				model_id, exercise_id, position, planned_sets, planned_reps,
				rest_seconds, notes, starter_load_kg
			)
			VALUES ($1, $2::uuid, $3, $4, $5, $6, $7, $8)`,
			modelID, it.ExerciseID, i+1, it.PlannedSets, strings.TrimSpace(it.PlannedReps),
			it.RestSeconds, it.Notes, it.StarterLoadKg,
		); err != nil {
			return fmt.Errorf("publish model item: %w", err)
		}
	}
	return nil
}

func (s *Service) GetModel(ctx context.Context, ownerID, modelID string) (*ModelDetail, error) {
	if modelID == "" {
		return nil, ErrNotFound
	}
	studioID, err := s.ownerTime(ctx, ownerID)
	if err != nil {
		return nil, err
	}

	var out ModelDetail
	err = s.db.QueryRowContext(ctx, `
		SELECT id::text, name FROM models
		WHERE id = $1 AND studio_id = $2 AND archived_at IS NULL`,
		modelID, studioID,
	).Scan(&out.ID, &out.Name)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("publish model: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT mi.id::text, mi.exercise_id::text, e.name, mi.position,
		       mi.planned_sets, mi.planned_reps, COALESCE(mi.starter_load_kg, 0)
		FROM model_items mi
		JOIN exercises e ON e.id = mi.exercise_id
		WHERE mi.model_id = $1
		ORDER BY mi.position`,
		modelID,
	)
	if err != nil {
		return nil, fmt.Errorf("publish model items: %w", err)
	}
	defer rows.Close()

	out.Items = []ModelItem{}
	for rows.Next() {
		var it ModelItem
		if err := rows.Scan(&it.ID, &it.ExerciseID, &it.Name, &it.Position, &it.PlannedSets, &it.PlannedReps, &it.StarterLoadKg); err != nil {
			return nil, fmt.Errorf("publish model items scan: %w", err)
		}
		out.Items = append(out.Items, it)
	}
	return &out, rows.Err()
}

func (s *Service) DraftFromLast(ctx context.Context, ownerID, modelID, personID, from string) (*Draft, error) {
	if modelID == "" || personID == "" {
		return nil, ErrInvalid
	}
	if from == "" {
		from = "last"
	}
	if from != "last" && from != "model" {
		return nil, ErrInvalid
	}
	studioID, err := s.ownerTime(ctx, ownerID)
	if err != nil {
		return nil, err
	}

	var n int
	if err := s.db.QueryRowContext(ctx, `
		SELECT count(*) FROM bonds
		WHERE person_id = $1 AND studio_id = $2 AND role = 'student' AND status = 'active'`,
		personID, studioID,
	).Scan(&n); err != nil {
		return nil, fmt.Errorf("publish draft bond: %w", err)
	}
	if n == 0 {
		return nil, ErrNotFound
	}

	var timeMatch int
	if err := s.db.QueryRowContext(ctx, `
		SELECT count(*) FROM models
		WHERE id = $1 AND studio_id = $2 AND archived_at IS NULL`,
		modelID, studioID,
	).Scan(&timeMatch); err != nil {
		return nil, fmt.Errorf("publish draft model: %w", err)
	}
	if timeMatch == 0 {
		return nil, ErrNotFound
	}

	tomorrow := s.now().AddDate(0, 0, 1).Format("2006-01-02")

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var draftID string
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO prescriptions (studio_id, person_id, model_id, for_date, status)
		VALUES ($1, $2, $3::uuid, $4::date, 'draft')
		RETURNING id::text`,
		studioID, personID, modelID, tomorrow,
	).Scan(&draftID); err != nil {
		return nil, fmt.Errorf("publish draft insert: %w", err)
	}

	if from == "model" {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO prescription_items (
				prescription_id, exercise_id, position, planned_sets, planned_reps,
				load_kg, rest_seconds, notes, load_source
			)
			SELECT $1, mi.exercise_id, mi.position, mi.planned_sets, mi.planned_reps,
			       COALESCE(mi.starter_load_kg, 0), mi.rest_seconds, mi.notes, 'starter'
			FROM model_items mi
			WHERE mi.model_id = $2::uuid`,
			draftID, modelID,
		); err != nil {
			return nil, fmt.Errorf("publish draft starter items: %w", err)
		}
	} else if _, err := tx.ExecContext(ctx, ItensComCargaDoCorpo,
		draftID, personID, studioID, tomorrow, modelID,
	); err != nil {
		return nil, fmt.Errorf("publish draft items: %w", err)
	}

	items, err := loadDraftItems(ctx, tx, draftID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &Draft{DraftID: draftID, Items: items}, nil
}

func loadDraftItems(ctx context.Context, q interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, draftID string) ([]DraftItem, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT pi.id::text, pi.exercise_id::text, e.name,
		       pi.planned_sets, pi.planned_reps, COALESCE(pi.load_kg, 0), pi.load_source,
		       pi.notes
		FROM prescription_items pi
		JOIN exercises e ON e.id = pi.exercise_id
		WHERE pi.prescription_id = $1
		ORDER BY pi.position`,
		draftID,
	)
	if err != nil {
		return nil, fmt.Errorf("publish draft list: %w", err)
	}
	defer rows.Close()

	items := []DraftItem{}
	for rows.Next() {
		var it DraftItem
		var notes sql.NullString
		if err := rows.Scan(&it.ID, &it.ExerciseID, &it.Name, &it.PlannedSets, &it.PlannedReps, &it.LoadKg, &it.LoadSource, &notes); err != nil {
			return nil, fmt.Errorf("publish draft list scan: %w", err)
		}
		if notes.Valid && strings.TrimSpace(notes.String) != "" {
			n := notes.String
			it.Notes = &n
		}
		items = append(items, it)
	}
	return items, rows.Err()
}

// SwapItem troca UM exercicio da Prescricao de uma pessoa por outro, e RE-RESOLVE a carga
// a partir do corpo dela naquele exercicio novo.
//
// A carga NAO atravessa a troca, e essa e a parte que importa: se a pessoa faz Supino com
// 40 e o personal troca por Leg press, herdar 40 poria 40 kg num exercicio que ela nunca
// fez e que usa outro padrao motor. O numero volta a ser resolvido do zero pela mesma
// cascata da publicacao — corpo primeiro, ficha anterior depois, e nada em ultimo caso — e
// com o mesmo carimbo por degrau: 'history' so quando existe serie executada NAQUELE
// exercicio, 'prescription' quando o numero so existe porque o personal ja o prescreveu.
//
// Aqui nao existe o degrau do starter do Modelo: o exercicio novo nao esta no Modelo, entao
// nao ha chute inicial para ele. Sem historico, a carga fica em zero e a tela mostra isso
// como ausencia — que e a resposta honesta para "eu nunca te vi fazer isso".
func (s *Service) SwapItem(ctx context.Context, ownerID, prescriptionID, itemID, novoExercicioID string) error {
	if prescriptionID == "" || itemID == "" || novoExercicioID == "" {
		return ErrNotFound
	}
	studioID, err := s.ownerTime(ctx, ownerID)
	if err != nil {
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var status, personID string
	err = tx.QueryRowContext(ctx, `
		SELECT status, person_id::text FROM prescriptions
		WHERE id = $1 AND studio_id = $2
		FOR UPDATE`,
		prescriptionID, studioID,
	).Scan(&status, &personID)
	if err == sql.ErrNoRows {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("publish swap status: %w", err)
	}
	// Ficha publicada nao se edita por baixo de quem ja esta treinando nela.
	if status != "draft" {
		return ErrInvalid
	}

	var pertence int
	if err := tx.QueryRowContext(ctx, `
		SELECT count(*) FROM exercises WHERE id = $1::uuid AND studio_id = $2`,
		novoExercicioID, studioID,
	).Scan(&pertence); err != nil {
		return fmt.Errorf("publish swap exercise: %w", err)
	}
	if pertence == 0 {
		return ErrNotFound
	}

	// O mesmo exercicio duas vezes na mesma ficha e engano de quem trocou.
	var repetido int
	if err := tx.QueryRowContext(ctx, `
		SELECT count(*) FROM prescription_items
		WHERE prescription_id = $1 AND exercise_id = $2::uuid AND id <> $3`,
		prescriptionID, novoExercicioID, itemID,
	).Scan(&repetido); err != nil {
		return fmt.Errorf("publish swap dup: %w", err)
	}
	if repetido > 0 {
		return ErrInvalid
	}

	res, err := tx.ExecContext(ctx, `
		UPDATE prescription_items pi
		SET exercise_id = $3::uuid,
		    load_kg = COALESCE(corpo.load_kg, ficha.load_kg, 0),
		    load_source = CASE
		        WHEN corpo.load_kg IS NOT NULL THEN 'history'
		        WHEN ficha.load_kg IS NOT NULL THEN 'prescription'
		        ELSE 'starter'
		    END
		FROM (SELECT 1) AS _
		LEFT JOIN LATERAL (
			SELECT ws.load_kg
			FROM workout_sets ws
			JOIN workout_sessions sess ON sess.id = ws.session_id
			WHERE sess.person_id = $4 AND sess.studio_id = $5
			  AND ws.exercise_id = $3::uuid AND ws.load_kg IS NOT NULL
			ORDER BY ws.performed_at DESC
			LIMIT 1
		) corpo ON true
		LEFT JOIN LATERAL (
			SELECT pi2.load_kg
			FROM prescriptions pr2
			JOIN prescription_items pi2
			  ON pi2.prescription_id = pr2.id AND pi2.exercise_id = $3::uuid
			WHERE pr2.person_id = $4 AND pr2.studio_id = $5 AND pr2.status = 'published'
			ORDER BY pr2.for_date DESC
			LIMIT 1
		) ficha ON true
		WHERE pi.id = $1 AND pi.prescription_id = $2`,
		itemID, prescriptionID, novoExercicioID, personID, studioID,
	)
	if err != nil {
		return fmt.Errorf("publish swap: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return tx.Commit()
}

func (s *Service) PatchItem(ctx context.Context, ownerID, prescriptionID, itemID string, patch ItemPatch) error {
	if prescriptionID == "" || itemID == "" {
		return ErrNotFound
	}
	if patch.PlannedSets <= 0 || patch.PlannedReps == "" || patch.LoadKg < 0 {
		return ErrInvalid
	}
	studioID, err := s.ownerTime(ctx, ownerID)
	if err != nil {
		return err
	}

	var status string
	err = s.db.QueryRowContext(ctx, `
		SELECT status FROM prescriptions
		WHERE id = $1 AND studio_id = $2`,
		prescriptionID, studioID,
	).Scan(&status)
	if err == sql.ErrNoRows {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("publish patch status: %w", err)
	}
	if status != "draft" {
		return ErrInvalid
	}

	var res sql.Result
	if patch.Notes == nil {
		res, err = s.db.ExecContext(ctx, `
			UPDATE prescription_items
			SET load_kg = $3, planned_sets = $4, planned_reps = $5, load_source = 'manual'
			WHERE id = $1 AND prescription_id = $2`,
			itemID, prescriptionID, patch.LoadKg, patch.PlannedSets, patch.PlannedReps,
		)
	} else {
		var notes any
		if n := strings.TrimSpace(*patch.Notes); n != "" {
			notes = n
		}
		res, err = s.db.ExecContext(ctx, `
			UPDATE prescription_items
			SET load_kg = $3, planned_sets = $4, planned_reps = $5, notes = $6, load_source = 'manual'
			WHERE id = $1 AND prescription_id = $2`,
			itemID, prescriptionID, patch.LoadKg, patch.PlannedSets, patch.PlannedReps, notes,
		)
	}
	if err != nil {
		return fmt.Errorf("publish patch: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// LimiteDaFrase e o mesmo teto de boas_vindas: uma linha, nao um texto. Quem julga o
// conteudo e internal/voz, que ja e o dono da regra dos outros dois slots de voz.
const LimiteDaFrase = 100

// Publish poe a Prescricao no ar — e, opcionalmente, carrega a FRASE que o personal
// escreveu para o dia.
//
// A frase e dele, literal, ou nao existe. Nao ha template de reserva: o /v1/today montava
// "<exercicio> em <carga>. Tecnica, nao ego." e a tela do aluno assinava aquilo com o rosto
// e o nome do personal. Frase vazia grava NULL, o /today devolve vazio e o bloco inteiro
// some da tela — calar e a unica reserva honesta para a voz de outra pessoa.
//
// A frase e do ATO de publicar, entao ela vale para todo mundo do lote: mesma estrutura,
// mesmo recado, cada corpo com a sua carga.
func (s *Service) Publish(ctx context.Context, ownerID, prescriptionID string, alsoPersonIDs []string, coachLine string) error {
	if prescriptionID == "" {
		return ErrInvalid
	}
	frase, ok := voz.Limpa(coachLine, LimiteDaFrase)
	if !ok {
		return ErrInvalid
	}
	studioID, err := s.ownerTime(ctx, ownerID)
	if err != nil {
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var (
		personID string
		modelID  string
		forDate  string
		status   string
	)
	err = tx.QueryRowContext(ctx, `
		SELECT person_id::text, model_id::text, for_date::text, status
		FROM prescriptions
		WHERE id = $1 AND studio_id = $2
		FOR UPDATE`,
		prescriptionID, studioID,
	).Scan(&personID, &modelID, &forDate, &status)
	if err == sql.ErrNoRows {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("publish load: %w", err)
	}
	if status != "draft" {
		return ErrInvalid
	}
	if len(forDate) > 10 {
		forDate = forDate[:10]
	}

	now := s.now()
	if err := supersedePublished(ctx, tx, personID, studioID, forDate); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE prescriptions
		SET status = 'published', published_at = $2, coach_line = nullif($3, ''), updated_at = now()
		WHERE id = $1 AND status = 'draft'`,
		prescriptionID, now, frase,
	); err != nil {
		return fmt.Errorf("publish draft: %w", err)
	}

	seen := map[string]bool{personID: true}
	for _, extraID := range alsoPersonIDs {
		if extraID == "" || seen[extraID] {
			continue
		}
		seen[extraID] = true
		if err := publishFromModelo(ctx, tx, studioID, extraID, modelID, forDate, frase, now); err != nil {
			if err == ErrNotFound {
				continue
			}
			return err
		}
	}

	return tx.Commit()
}

func supersedePublished(ctx context.Context, tx *sql.Tx, personID, studioID, forDate string) error {
	if _, err := tx.ExecContext(ctx, `
		UPDATE prescriptions
		SET status = 'superseded', updated_at = now()
		WHERE person_id = $1 AND studio_id = $2 AND for_date = $3::date AND status = 'published'`,
		personID, studioID, forDate,
	); err != nil {
		return fmt.Errorf("publish supersede: %w", err)
	}
	return nil
}

func publishFromModelo(ctx context.Context, tx *sql.Tx, studioID, personID, modelID, forDate, frase string, now time.Time) error {
	var n int
	if err := tx.QueryRowContext(ctx, `
		SELECT count(*) FROM bonds
		WHERE person_id = $1 AND studio_id = $2 AND role = 'student' AND status = 'active'`,
		personID, studioID,
	).Scan(&n); err != nil {
		return fmt.Errorf("publish extra bond: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}

	if err := supersedePublished(ctx, tx, personID, studioID, forDate); err != nil {
		return err
	}

	var prID string
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO prescriptions (studio_id, person_id, model_id, for_date, status, published_at, coach_line)
		VALUES ($1, $2, $3::uuid, $4::date, 'published', $5, nullif($6, ''))
		RETURNING id::text`,
		studioID, personID, modelID, forDate, now, frase,
	).Scan(&prID); err != nil {
		return fmt.Errorf("publish extra: %w", err)
	}

	if _, err := tx.ExecContext(ctx, ItensComCargaDoCorpo,
		prID, personID, studioID, forDate, modelID,
	); err != nil {
		return fmt.Errorf("publish extra items: %w", err)
	}
	return nil
}
