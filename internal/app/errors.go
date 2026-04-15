package app

import openbindings "github.com/openbindings/openbindings-go"

// Error is the shared structured error type used throughout the app layer.
type Error = openbindings.ExecuteError
