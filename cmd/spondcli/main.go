// Command spondcli is a terminal client for Spond.
//
// Without arguments it starts an interactive UI; subcommands list and export
// members and search posts. Run "spondcli help" for details.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/asbjorn/spondcli/internal/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	err := cli.Run(ctx, os.Args[1:])
	stop()
	if err == nil {
		return
	}
	fmt.Fprintln(os.Stderr, "spondcli:", err)
	if errors.Is(err, cli.ErrUsage) {
		os.Exit(2)
	}
	os.Exit(1)
}
