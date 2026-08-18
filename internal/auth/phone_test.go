package auth

import "testing"

func TestNormalizePhone(t *testing.T) {
	got, err := NormalizePhone("11900000001")
	if err != nil {
		t.Fatal(err)
	}
	if got != "+5511900000001" {
		t.Fatalf("got %s", got)
	}
	got, err = NormalizePhone("+55 11 90000-0001")
	if err != nil {
		t.Fatal(err)
	}
	if got != "+5511900000001" {
		t.Fatalf("got %s", got)
	}
}
