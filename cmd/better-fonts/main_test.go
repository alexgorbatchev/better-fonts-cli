package main

import (
	"os"
	"testing"
)

func TestExecuteCommand(t *testing.T) {
	if err := execute([]string{"--version"}); err != nil {
		t.Fatalf("execute --version failed: %v", err)
	}

	if err := execute([]string{"list"}); err != nil {
		t.Fatalf("execute list failed: %v", err)
	}
}

func TestExecuteCommand_Error(t *testing.T) {
	if err := execute([]string{"--invalid-flag-xyz"}); err == nil {
		t.Fatalf("expected error on invalid flag")
	}
}

func TestMainFunction(t *testing.T) {
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	os.Args = []string{"better-fonts", "--version"}
	main()
}
