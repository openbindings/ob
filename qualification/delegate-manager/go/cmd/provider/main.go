// Command provider is the independently authored synthetic delegate used by
// the Delegate Manager consumer harness. It is not derived from ob's source:
// its operations carry unfamiliar local keys adopted through exact shared
// aliases, it advertises an unrequested extra capability (synthesis), it
// serves a genuine streaming invokeBinding over the binding-invoker frame
// protocol on a WebSocket, and it keeps separate support / work / listing /
// credential counters that a consumer reads back to prove what happened.
//
// Usage: provider --accepted <invoke-accepted-interface.json> --extra <synthesize-accepted-interface.json> --obi <out.obi.json>
//
// It prints one JSON line {"url": ..., "counters": ...} once listening and
// serves until its stdin closes. Counters are read at GET /counters.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/coder/websocket"
)

type document = map[string]any

const supportedToken = "example.consumer-stream@1"

func main() {
	accepted := flag.String("accepted", "", "the invoke role's accepted interface (from listRoles)")
	extra := flag.String("extra", "", "the synthesize role's accepted interface: an unrequested extra capability")
	out := flag.String("obi", "", "where to write the provider's OBI value")
	flag.Parse()
	if *accepted == "" || *out == "" {
		log.Fatal("--accepted and --obi are required")
	}
	invokeRole := mustRead(*accepted)
	var extraRole document
	if *extra != "" {
		extraRole = mustRead(*extra)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatal(err)
	}
	base := "http://" + listener.Addr().String()
	var counters struct {
		mu           sync.Mutex
		Support      int64            `json:"support"`
		Listing      int64            `json:"listing"`
		Work         int64            `json:"work"`
		Extra        int64            `json:"extra"`
		Credentials  int64            `json:"credentialBearing"`
		Documents    int64            `json:"documents"`
		Tokens       []string         `json:"supportTokens"`
		Contexts     []map[string]any `json:"workContexts"`
		OpenFrames   []map[string]any `json:"openFrames"`
		WorkSelector string           `json:"-"`
	}
	var unexpected atomic.Int64

	operations := document{}
	bindings := document{}
	paths := document{}
	// Adopt every expected key through an unfamiliar local key + exact alias;
	// keep a bare-name decoy with the same shape and a DECOY path so a
	// bare-name lookup would be observable.
	adopt := func(role document, prefix string, routes map[string]string) {
		ops, _ := role["operations"].(document)
		for key, raw := range ops {
			op, _ := raw.(document)
			local := prefix + "." + strings.ReplaceAll(key, ".", "_")
			copied := document{}
			for k, v := range op {
				copied[k] = v
			}
			copied["aliases"] = []any{key}
			operations[local] = copied
			bare := key[strings.LastIndex(key, ".")+1:]
			decoy := document{}
			for k, v := range op {
				if k != "aliases" {
					decoy[k] = v
				}
			}
			operations[bare] = decoy
			if route, ok := routes[key]; ok {
				bindings[local] = document{"operation": local, "source": "api", "selector": "#/paths/" + pointerToken(route) + "/post", "inputTransform": `{"body": $$}`}
				bindings["decoy-"+bare] = document{"operation": bare, "source": "api", "selector": "#/paths/" + pointerToken("/decoy/"+bare) + "/post", "inputTransform": `{"body": $$}`}
				paths["/"+strings.TrimPrefix(route, "/")] = jsonPost()
				paths["/decoy/"+bare] = jsonPost()
			}
		}
	}
	invokeRoutes := map[string]string{
		"openbindings.binding-invoker.checkBindingSpecs": "/support/check",
		"openbindings.binding-invoker.listBindingSpecs":  "/support/list",
	}
	adopt(invokeRole, "acme.stream", invokeRoutes)
	// The frame operation binds to the AsyncAPI channel, not to OpenAPI.
	for key := range operations {
		if strings.HasSuffix(key, "binding-invoker_invokeBinding") {
			bindings[key] = document{"operation": key, "source": "frames", "selector": "#/operations/invokeBinding"}
			counters.WorkSelector = key
		}
	}
	if extraRole != nil {
		adopt(extraRole, "acme.author", map[string]string{
			"openbindings.interface-synthesizer.checkBindingSpecs":   "/extra/check",
			"openbindings.interface-synthesizer.listBindingSpecs":    "/extra/list",
			"openbindings.interface-synthesizer.synthesizeInterface": "/extra/synthesize",
		})
	}
	schemas := document{}
	for _, role := range []document{invokeRole, extraRole} {
		if role == nil {
			continue
		}
		if s, ok := role["schemas"].(document); ok {
			for k, v := range s {
				schemas[k] = v
			}
		}
	}
	artifact := document{"openapi": "3.1.0", "info": document{"title": "Consumer harness provider", "version": "1"}, "servers": []any{document{"url": base}}, "paths": paths}
	artifactJSON, _ := json.Marshal(artifact)
	obi := document{
		"openbindings": "0.2.0",
		"name":         "Consumer harness provider",
		"description":  "sensitive-provider-document-marker",
		"schemas":      schemas,
		"operations":   operations,
		"sources": document{
			"api":    document{"bindingSpec": "openbindings.openapi-3.1@1", "content": json.RawMessage(artifactJSON)},
			"frames": document{"bindingSpec": "openbindings.asyncapi@1", "location": base + "/asyncapi.yaml"},
		},
		"bindings": bindings,
	}
	obiJSON, _ := json.MarshalIndent(obi, "", "  ")
	if err := os.WriteFile(*out, obiJSON, 0600); err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()
	writeJSON := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	}
	readTokens := func(r *http.Request) []string {
		var input struct {
			BindingSpecs []string `json:"bindingSpecs"`
		}
		_ = json.NewDecoder(r.Body).Decode(&input)
		return input.BindingSpecs
	}
	mux.HandleFunc("POST /support/check", func(w http.ResponseWriter, r *http.Request) {
		tokens := readTokens(r)
		counters.mu.Lock()
		counters.Support++
		counters.Tokens = append(counters.Tokens, tokens...)
		if r.Header.Get("Authorization") != "" {
			counters.Credentials++
		}
		counters.mu.Unlock()
		verdicts := []any{}
		for _, token := range tokens {
			verdicts = append(verdicts, document{"bindingSpec": token, "supported": token == supportedToken})
		}
		writeJSON(w, verdicts)
	})
	mux.HandleFunc("POST /support/list", func(w http.ResponseWriter, r *http.Request) {
		counters.mu.Lock()
		counters.Listing++
		counters.mu.Unlock()
		writeJSON(w, []any{document{"bindingSpec": supportedToken}})
	})
	for _, path := range []string{"POST /extra/check", "POST /extra/list", "POST /extra/synthesize"} {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			counters.mu.Lock()
			counters.Extra++
			counters.mu.Unlock()
			http.Error(w, "unrequested capability must never be consulted", http.StatusInternalServerError)
		})
	}
	mux.HandleFunc("POST /decoy/", func(w http.ResponseWriter, r *http.Request) {
		unexpected.Add(1)
		http.Error(w, "bare-name decoy reached", http.StatusInternalServerError)
	})
	mux.HandleFunc("GET /asyncapi.yaml", func(w http.ResponseWriter, r *http.Request) {
		counters.mu.Lock()
		counters.Documents++
		counters.mu.Unlock()
		w.Header().Set("Content-Type", "application/yaml")
		_, _ = io.WriteString(w, strings.ReplaceAll(asyncAPI, "${HOST}", listener.Addr().String()))
	})
	mux.HandleFunc("GET /bindings/invoke", func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		defer conn.CloseNow()
		ctx := r.Context()
		opened := false
		for {
			_, raw, err := conn.Read(ctx)
			if err != nil {
				return
			}
			var frame document
			dec := json.NewDecoder(strings.NewReader(string(raw)))
			dec.UseNumber()
			if err := dec.Decode(&frame); err != nil {
				return
			}
			var reply document
			switch frame["kind"] {
			case "open":
				input, _ := frame["input"].(document)
				counters.mu.Lock()
				counters.Work++
				counters.OpenFrames = append(counters.OpenFrames, input)
				if c, _ := input["context"].(document); c != nil {
					counters.Contexts = append(counters.Contexts, c)
				}
				counters.mu.Unlock()
				opened = true
				continue
			case "input":
				if !opened {
					return
				}
				// Echo the exact value back as one output frame.
				reply = document{"kind": "output", "value": frame["value"]}
			case "close":
				reply = document{"kind": "complete"}
			default:
				return
			}
			data, _ := json.Marshal(reply)
			if err := conn.Write(ctx, websocket.MessageText, data); err != nil {
				return
			}
			if reply["kind"] == "complete" {
				return
			}
		}
	})
	mux.HandleFunc("GET /counters", func(w http.ResponseWriter, r *http.Request) {
		counters.mu.Lock()
		defer counters.mu.Unlock()
		writeJSON(w, document{
			"support": counters.Support, "listing": counters.Listing, "work": counters.Work, "extra": counters.Extra,
			"credentialBearing": counters.Credentials, "documents": counters.Documents, "supportTokens": counters.Tokens,
			"workContexts": counters.Contexts, "openFrames": counters.OpenFrames, "decoyCalls": unexpected.Load(),
		})
	})
	server := &http.Server{Handler: mux}
	go func() { _ = server.Serve(listener) }()
	fmt.Printf(`{"url":%q,"obi":%q,"token":%q}`+"\n", base, *out, supportedToken)
	_, _ = io.Copy(io.Discard, os.Stdin)
	_ = server.Shutdown(context.Background())
}

func jsonPost() document {
	return document{"post": document{
		"requestBody": document{"content": document{"application/json": document{"schema": document{}}}},
		"responses":   document{"200": document{"description": "ok", "content": document{"application/json": document{"schema": document{}}}}},
	}}
}

func mustRead(path string) document {
	data, err := os.ReadFile(path)
	if err != nil {
		log.Fatal(err)
	}
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.UseNumber()
	var value document
	if err := dec.Decode(&value); err != nil {
		log.Fatal(err)
	}
	return value
}

// asyncAPI describes the provider's frame channel. It is protocol data for the
// binding-invoker frame protocol, authored for this fixture.
const asyncAPI = `asyncapi: 3.0.0
info:
  title: Consumer harness provider streams
  version: "1"
servers:
  local:
    host: ${HOST}
    protocol: ws
channels:
  bindingsInvoke:
    address: /bindings/invoke
    bindings:
      ws:
        method: GET
    messages:
      inputFrame:
        name: inputFrame
        contentType: application/json
        payload:
          type: object
      outputFrame:
        name: outputFrame
        contentType: application/json
        payload:
          type: object
operations:
  invokeBinding:
    action: receive
    channel:
      $ref: '#/channels/bindingsInvoke'
    messages:
      - $ref: '#/channels/bindingsInvoke/messages/inputFrame'
    reply:
      channel:
        $ref: '#/channels/bindingsInvoke'
      messages:
        - $ref: '#/channels/bindingsInvoke/messages/outputFrame'
`

// pointerToken escapes one JSON Pointer reference token (RFC 6901): "~" then "/".
func pointerToken(token string) string {
	return strings.ReplaceAll(strings.ReplaceAll(token, "~", "~0"), "/", "~1")
}
