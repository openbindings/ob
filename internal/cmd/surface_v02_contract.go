package cmd

// v02Contract classifies the facade's result without implying that any
// underlying handler exists on this branch. The destination is the proposed
// real CLI contract; the preview always returns a placeholder exit instead.
type v02Contract struct {
	Result      string
	Destination string
}

var v02Contracts = map[string]v02Contract{
	"new":               {"obi", "canonical JSON stdout or -o"},
	"show":              {"report-or-obi", "stdout or -o"},
	"validate":          {"verdict-report", "stdout or -o; --quiet emits nothing"},
	"diff":              {"structural-report", "stdout or -o"},
	"patch":             {"obi", "canonical JSON stdout or -o"},
	"source.add":        {"obi", "canonical JSON stdout or -o"},
	"source.import":     {"obi", "canonical JSON stdout or -o"},
	"source.inspect":    {"handler-report", "stdout or -o"},
	"source.list":       {"report", "stdout or -o"},
	"source.pull":       {"obi", "canonical JSON stdout or -o"},
	"source.remove":     {"obi", "canonical JSON stdout or -o"},
	"source.show":       {"report", "stdout or -o"},
	"source.synthesize": {"patch-or-obi", "stdout or -o; --apply selects OBI"},
	"binding.add":       {"obi", "canonical JSON stdout or -o"},
	"binding.invoke":    {"ndjson-stream", "stdout only; diagnostics stderr"},
	"binding.list":      {"report", "stdout or -o"},
	"binding.remove":    {"obi", "canonical JSON stdout or -o"},
	"binding.show":      {"report", "stdout or -o"},
	"dependency.add":    {"obi", "canonical JSON stdout or -o"},
	"dependency.list":   {"report", "stdout or -o"},
	"dependency.remove": {"obi", "canonical JSON stdout or -o"},
	"dependency.show":   {"report", "stdout or -o"},
	"operation.add":     {"obi", "canonical JSON stdout or -o"},
	"operation.list":    {"report", "stdout or -o"},
	"operation.remove":  {"obi", "canonical JSON stdout or -o"},
	"operation.show":    {"report", "stdout or -o"},
	"schema.add":        {"obi", "canonical JSON stdout or -o"},
	"schema.list":       {"report", "stdout or -o"},
	"schema.remove":     {"obi", "canonical JSON stdout or -o"},
	"schema.show":       {"report", "stdout or -o"},
	"kind.check":        {"local-capability-report", "stdout or -o"},
	"kind.list":         {"local-capability-report", "stdout or -o"},
}
