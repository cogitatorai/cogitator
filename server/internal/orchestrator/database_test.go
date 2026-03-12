package orchestrator

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestSchemaHasOperatorColumn(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var isOp bool
	err = db.db.QueryRow(`SELECT is_operator FROM accounts LIMIT 0`).Scan(&isOp)
	if err != nil && err != sql.ErrNoRows {
		t.Fatalf("is_operator column missing or wrong type: %v", err)
	}
}

func TestPromoteOperator(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Insert a test account.
	_, err = db.db.Exec(`INSERT INTO accounts (id, email, password_hash) VALUES ('acct_op', 'op@example.com', 'hash')`)
	if err != nil {
		t.Fatalf("insert account: %v", err)
	}

	if err := db.PromoteOperator("op@example.com"); err != nil {
		t.Fatalf("PromoteOperator: %v", err)
	}

	var isOp bool
	db.db.QueryRow(`SELECT is_operator FROM accounts WHERE id = 'acct_op'`).Scan(&isOp)
	if !isOp {
		t.Fatal("expected is_operator = true after promotion")
	}
}

func TestPromoteOperator_MissingAccount(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	err = db.PromoteOperator("nobody@example.com")
	if err == nil {
		t.Fatal("expected error for non-existent account, got nil")
	}
}
