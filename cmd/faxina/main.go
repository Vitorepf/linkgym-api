// Faxina de rastro de teste no banco de DESENVOLVIMENTO. Não vai para produção: existe
// porque provar a porta nova por curl cria uma pessoa de verdade, e os testes do seed
// cravam a turma em 28. Recebe telefones como argumento e apaga só o que pertence a eles.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/Vitorepf/linkgym-api/internal/db"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("uso: go run ./cmd/faxina +5511900000000 [...]")
		return
	}
	d, err := db.Open(os.Getenv("DATABASE_URL"))
	if err != nil {
		panic(err)
	}
	defer d.Close()
	ctx := context.Background()
	for _, fone := range os.Args[1:] {
		for _, q := range []string{
			`UPDATE people SET active_bond_id = NULL WHERE phone = $1`,
			`DELETE FROM auth_sessions WHERE person_id IN (SELECT id FROM people WHERE phone = $1)`,
			`DELETE FROM bonds WHERE person_id IN (SELECT id FROM people WHERE phone = $1)`,
			`DELETE FROM login_codes WHERE phone = $1`,
			`DELETE FROM invites WHERE phone = $1`,
			`DELETE FROM people WHERE phone = $1`,
		} {
			if _, err := d.ExecContext(ctx, q, fone); err != nil {
				fmt.Println("ERRO:", err)
			}
		}
		fmt.Println("limpo:", fone)
	}
	var n int
	_ = d.QueryRowContext(ctx, `SELECT count(*) FROM bonds WHERE role='student' AND status='active'`).Scan(&n)
	fmt.Println("alunos ativos:", n)
}
