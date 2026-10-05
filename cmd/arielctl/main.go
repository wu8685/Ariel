package main

import (
	"context"
	"fmt"
	"os"

	"github.com/wu8685/Ariel/internal/startup"
)

func main() {
	root, err := os.Getwd()
	if err == nil {
		err = startup.Run(context.Background(), root, os.Args[1:], os.Stdout)
	}
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "Ariel:", err)
		os.Exit(1)
	}
}
