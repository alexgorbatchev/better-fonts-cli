package main

import (
	"fmt"
	"os"

	"github.com/alexgorbatchev/better-fonts/internal/runner"
)

// version is set during build via -ldflags "-X main.version=..."
var version = "dev"

func main() {
	if err := execute(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func execute(args []string) error {
	cmd := runner.NewRootCommand(version)
	cmd.SetArgs(args)
	return cmd.Execute()
}
