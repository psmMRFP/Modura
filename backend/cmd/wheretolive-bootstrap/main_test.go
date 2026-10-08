package main

import (
	"strings"
	"testing"
)

func TestReadPassword(t *testing.T) {
	password, err := readPassword(strings.NewReader("a secure bootstrap password\nignored"))
	if err != nil {
		t.Fatal(err)
	}
	if password != "a secure bootstrap password" {
		t.Fatalf("password was not read as one line")
	}
}

func TestReadPasswordRejectsEmptyInput(t *testing.T) {
	if _, err := readPassword(strings.NewReader("\n")); err == nil {
		t.Fatal("expected empty password to fail")
	}
}
