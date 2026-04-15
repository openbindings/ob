package main

import (
	"fmt"
	"os"

	"github.com/openbindings/ob/internal/cmd"
)

func main() {
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
			os.Exit(ee.ExitCode())
		}

		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}
