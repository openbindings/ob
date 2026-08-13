package app

// Error is the app/report layer's descriptive error carrier. It is not the
// OpenBindings invocation error wire type: invocation boundaries deliberately
// expose only a binding-owned code and optional application-authored data.
type Error struct {
	Code        string `json:"code"`
	Message     string `json:"message"`
	Data        any    `json:"-"`
	DataPresent bool   `json:"-"`
}
