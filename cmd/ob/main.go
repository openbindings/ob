package main

import (
	"fmt"
	"os"

	"github.com/openbindings/ob/internal/app"
	"github.com/openbindings/ob/internal/cmd"
)

func main() {
	// Fires an async GitHub releases probe. The returned function blocks
	// briefly on exit to print the notification if a newer ob is available.
	// No-op on CI, when OB_NO_UPDATE_CHECK=1 is set, or for dev builds.
	notifyUpdate := app.StartUpdateCheck(app.OBVersion)
	defer notifyUpdate()

	root := cmd.NewRoot()
	if err := root.Execute(); err != nil {
		if ee, ok := err.(interface {
			error
			ExitCode() int
			UseStderr() bool
		}); ok {
			msg := ee.Error()
			if msg != "" {
				if ee.UseStderr() {
					fmt.Fprintln(os.Stderr, msg)
				} else {
					fmt.Fprintln(os.Stdout, msg)
				}
			}
			notifyUpdate()
			os.Exit(ee.ExitCode())
		}

		fmt.Fprintln(os.Stderr, err.Error())
		notifyUpdate()
		os.Exit(1)
	}
}
