package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	openbindings "github.com/openbindings/openbindings-go"

	"github.com/openbindings/ob/internal/app"
)

// decodeRequest enforces the JSON portion of the published contract: a bounded
// body, exactly one JSON value, and no undeclared object properties.
func decodeRequest(w http.ResponseWriter, r *http.Request, dst any) bool {
	if !validateJSONMediaType(w, r) {
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeRequestDecodeError(w, err)
		return false
	}
	if err := ensureJSONEOF(dec); err != nil {
		writeRequestDecodeError(w, err)
		return false
	}
	return true
}

func validateJSONMediaType(w http.ResponseWriter, r *http.Request) bool {
	contentType := r.Header.Get("Content-Type")
	if contentType == "" {
		return true
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil || (mediaType != "application/json" && !strings.HasSuffix(mediaType, "+json")) {
		writeErrorJSON(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "request body must be JSON")
		return false
	}
	return true
}

func writeRequestDecodeError(w http.ResponseWriter, err error) {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		writeErrorJSON(w, http.StatusRequestEntityTooLarge, "request_too_large", fmt.Sprintf("request body exceeds %d bytes", maxRequestBodyBytes))
		return
	}
	writeErrorJSON(w, http.StatusBadRequest, "invalid_request", err.Error())
}

func ensureJSONEOF(dec *json.Decoder) error {
	var extra any
	if err := dec.Decode(&extra); err == io.EOF {
		return nil
	} else if err != nil {
		return err
	}
	return fmt.Errorf("request body must contain exactly one JSON value")
}

func requireInterface(w http.ResponseWriter, iface *openbindings.Interface) bool {
	if iface != nil {
		return true
	}
	writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "interface is required")
	return false
}

// editInterfaceFile adapts the file-oriented application editing core to the
// stateless wire contract. Each request gets an isolated 0600 work document;
// only the resulting document is returned and no caller path is accepted.
func editInterfaceFile(iface *openbindings.Interface, edit func(string) error) (*openbindings.Interface, error) {
	f, err := os.CreateTemp("", "ob-start-interface-*.json")
	if err != nil {
		return nil, fmt.Errorf("create work document: %w", err)
	}
	path := f.Name()
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return nil, fmt.Errorf("close work document: %w", err)
	}
	defer os.Remove(path)

	if err := app.WriteInterfaceFile(path, iface); err != nil {
		return nil, err
	}
	if err := edit(path); err != nil {
		return nil, err
	}
	return app.ResolveInterface(path)
}

func writeEditResult(w http.ResponseWriter, iface *openbindings.Interface, err error) {
	if err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "edit_failed", err.Error())
		return
	}
	writeOBI(w, http.StatusOK, iface)
}

func handleNewInterface(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name         string `json:"name,omitempty"`
		Version      string `json:"version,omitempty"`
		Description  string `json:"description,omitempty"`
		OpenBindings string `json:"openbindings,omitempty"`
	}
	if !decodeRequest(w, r, &body) {
		return
	}
	version := body.OpenBindings
	if version == "" {
		version = openbindings.MaxTestedVersion
	}
	writeOBI(w, http.StatusCreated, &openbindings.Interface{
		OpenBindings: version,
		Name:         body.Name,
		Version:      body.Version,
		Description:  body.Description,
		Operations:   map[string]openbindings.Operation{},
	})
}

func handleSetMetadata(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Interface   *openbindings.Interface `json:"interface"`
		Name        *string                 `json:"name,omitempty"`
		Version     *string                 `json:"version,omitempty"`
		Description *string                 `json:"description,omitempty"`
	}
	if !decodeRequest(w, r, &body) || !requireInterface(w, body.Interface) {
		return
	}
	result, err := editInterfaceFile(body.Interface, func(path string) error {
		_, err := app.MetaSet(app.MetaSetInput{
			Path: path, Name: body.Name, Version: body.Version, Description: body.Description,
		})
		return err
	})
	writeEditResult(w, result, err)
}

func handlePurifyInterface(w http.ResponseWriter, r *http.Request) {
	var body openbindings.Interface
	if !decodeRequest(w, r, &body) {
		return
	}
	app.StripAllXOB(&body)
	writeOBI(w, http.StatusOK, &body)
}

func handleAddSource(w http.ResponseWriter, r *http.Request) {
	var body app.AddInterfaceSourceInput
	if !decodeRequest(w, r, &body) || !requireInterface(w, body.Interface) {
		return
	}
	result, err := app.AddInterfaceSource(body)
	if err != nil {
		var conflict *app.SourceExistsError
		if errors.As(err, &conflict) {
			writeErrorJSON(w, http.StatusConflict, "source_exists", err.Error())
			return
		}
		writeErrorJSON(w, http.StatusBadRequest, "source_failed", err.Error())
		return
	}
	writeOBI(w, http.StatusOK, result)
}

func handleRemoveSource(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Interface *openbindings.Interface `json:"interface"`
		Key       string                  `json:"key"`
	}
	if !decodeRequest(w, r, &body) || !requireInterface(w, body.Interface) {
		return
	}
	result, err := editInterfaceFile(body.Interface, func(path string) error {
		_, err := app.SourceRemove(path, body.Key)
		return err
	})
	writeEditResult(w, result, err)
}

func handleListSources(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Interface *openbindings.Interface `json:"interface"`
	}
	if !decodeRequest(w, r, &body) || !requireInterface(w, body.Interface) {
		return
	}
	var out app.SourceListOutput
	_, err := editInterfaceFile(body.Interface, func(path string) error {
		var err error
		out, err = app.SourceList(path)
		return err
	})
	if err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "list_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func handlePullSource(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Interface  *openbindings.Interface `json:"interface"`
		SourceKeys []string                `json:"sourceKeys,omitempty"`
	}
	if !decodeRequest(w, r, &body) || !requireInterface(w, body.Interface) {
		return
	}
	if relative := relativeTrackedSourceRefs(body.Interface, body.SourceKeys); len(relative) > 0 {
		writeErrorJSON(w, http.StatusBadRequest, "relative_source_reference",
			"an inline interface has no base directory; embed these sources or use absolute source references before pulling: "+strings.Join(relative, ", "))
		return
	}
	var out app.SourcePullOutput
	_, err := editInterfaceFile(body.Interface, func(path string) error {
		var err error
		out, err = app.SourcePull(app.SourcePullInput{OBIPath: path, SourceKeys: body.SourceKeys})
		return err
	})
	if err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "pull_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func relativeTrackedSourceRefs(iface *openbindings.Interface, selected []string) []string {
	wanted := map[string]bool{}
	for _, key := range selected {
		wanted[key] = true
	}
	var relative []string
	for key, source := range iface.Sources {
		if len(wanted) > 0 && !wanted[key] {
			continue
		}
		meta, err := app.GetSourceMeta(source)
		if err != nil || meta == nil || meta.Ref == "" {
			continue
		}
		parsed, err := url.Parse(meta.Ref)
		if err == nil && parsed.Scheme == "" && !filepath.IsAbs(meta.Ref) {
			relative = append(relative, key)
		}
	}
	sort.Strings(relative)
	return relative
}

func handleListBindings(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Interface *openbindings.Interface `json:"interface"`
		Operation string                  `json:"operation,omitempty"`
	}
	if !decodeRequest(w, r, &body) || !requireInterface(w, body.Interface) {
		return
	}
	var out app.BindingListOutput
	_, err := editInterfaceFile(body.Interface, func(path string) error {
		var err error
		out, err = app.BindingList(path, body.Operation)
		return err
	})
	if err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "list_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func handleAddOperation(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Interface   *openbindings.Interface `json:"interface"`
		Key         string                  `json:"key"`
		Aliases     []string                `json:"aliases,omitempty"`
		Description string                  `json:"description,omitempty"`
		Tags        []string                `json:"tags,omitempty"`
		Input       openbindings.JSONSchema `json:"input,omitempty"`
		Output      openbindings.JSONSchema `json:"output,omitempty"`
		Idempotent  *bool                   `json:"idempotent,omitempty"`
	}
	if !decodeRequest(w, r, &body) || !requireInterface(w, body.Interface) {
		return
	}
	result, err := editInterfaceFile(body.Interface, func(path string) error {
		_, err := app.OperationAdd(app.OperationAddInput{
			OBIPath: path, Key: body.Key, Aliases: body.Aliases, Description: body.Description,
			Tags: body.Tags, Input: body.Input, Output: body.Output, Idempotent: body.Idempotent,
		})
		return err
	})
	writeEditResult(w, result, err)
}

func handleSetOperation(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Interface   *openbindings.Interface `json:"interface"`
		Operation   string                  `json:"operation"`
		Description *string                 `json:"description,omitempty"`
		Idempotent  *bool                   `json:"idempotent,omitempty"`
		Deprecated  *bool                   `json:"deprecated,omitempty"`
		Input       openbindings.JSONSchema `json:"input,omitempty"`
		Output      openbindings.JSONSchema `json:"output,omitempty"`
		AddTags     []string                `json:"addTags,omitempty"`
		RemoveTags  []string                `json:"removeTags,omitempty"`
		Own         bool                    `json:"own,omitempty"`
	}
	if !decodeRequest(w, r, &body) || !requireInterface(w, body.Interface) {
		return
	}
	result, err := editInterfaceFile(body.Interface, func(path string) error {
		_, err := app.OperationSet(app.OperationSetInput{
			OBIPath: path, Op: body.Operation, Description: body.Description,
			Idempotent: body.Idempotent, Deprecated: body.Deprecated, Input: body.Input,
			Output: body.Output, AddTags: body.AddTags, RemoveTags: body.RemoveTags, Own: body.Own,
		})
		return err
	})
	writeEditResult(w, result, err)
}

func handleDetachOperation(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Interface *openbindings.Interface `json:"interface"`
		Operation string                  `json:"operation"`
	}
	if !decodeRequest(w, r, &body) || !requireInterface(w, body.Interface) {
		return
	}
	result, err := editInterfaceFile(body.Interface, func(path string) error {
		_, err := app.OperationDetach(path, body.Operation)
		return err
	})
	writeEditResult(w, result, err)
}

func handleRenameOperation(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Interface *openbindings.Interface `json:"interface"`
		OldKey    string                  `json:"oldKey"`
		NewKey    string                  `json:"newKey"`
	}
	if !decodeRequest(w, r, &body) || !requireInterface(w, body.Interface) {
		return
	}
	result, err := editInterfaceFile(body.Interface, func(path string) error {
		_, err := app.OperationRename(path, body.OldKey, body.NewKey)
		return err
	})
	writeEditResult(w, result, err)
}

func handleRemoveOperation(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Interface *openbindings.Interface `json:"interface"`
		Keys      []string                `json:"keys"`
		Force     bool                    `json:"force,omitempty"`
	}
	if !decodeRequest(w, r, &body) || !requireInterface(w, body.Interface) {
		return
	}
	result, err := editInterfaceFile(body.Interface, func(path string) error {
		_, err := app.OperationRemove(path, body.Keys, body.Force)
		return err
	})
	writeEditResult(w, result, err)
}

func handleListOperations(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Interface *openbindings.Interface `json:"interface"`
		Tag       string                  `json:"tag,omitempty"`
	}
	if !decodeRequest(w, r, &body) || !requireInterface(w, body.Interface) {
		return
	}
	var out app.OperationListOutput
	_, err := editInterfaceFile(body.Interface, func(path string) error {
		var err error
		out, err = app.OperationList(path, body.Tag)
		return err
	})
	if err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "list_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func handleBindOperation(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Interface       *openbindings.Interface `json:"interface"`
		Operation       string                  `json:"operation"`
		Source          string                  `json:"source"`
		Ref             string                  `json:"ref"`
		Preference      *float64                `json:"preference,omitempty"`
		InputTransform  string                  `json:"inputTransform,omitempty"`
		OutputTransform string                  `json:"outputTransform,omitempty"`
		TransformStub   bool                    `json:"transformStub,omitempty"`
		Force           bool                    `json:"force,omitempty"`
	}
	if !decodeRequest(w, r, &body) || !requireInterface(w, body.Interface) {
		return
	}
	result, err := editInterfaceFile(body.Interface, func(path string) error {
		_, err := app.OperationBind(app.OperationBindInput{
			OBIPath: path, Op: body.Operation, Source: body.Source, Ref: body.Ref,
			Preference: body.Preference, InputTransform: body.InputTransform,
			OutputTransform: body.OutputTransform, TransformStub: body.TransformStub, Force: body.Force,
		})
		return err
	})
	writeEditResult(w, result, err)
}

func handleUnbindOperation(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Interface *openbindings.Interface `json:"interface"`
		Operation string                  `json:"operation"`
		Source    string                  `json:"source"`
		Binding   string                  `json:"binding"`
	}
	if !decodeRequest(w, r, &body) || !requireInterface(w, body.Interface) {
		return
	}
	if body.Binding != "" && (body.Operation != "" || body.Source != "") {
		writeErrorJSON(
			w,
			http.StatusBadRequest,
			"invalid_request",
			"provide either binding or operation and source, not both",
		)
		return
	}
	result, err := editInterfaceFile(body.Interface, func(path string) error {
		var err error
		if body.Binding != "" {
			_, err = app.OperationUnbindBinding(path, body.Binding)
		} else {
			_, err = app.OperationUnbind(path, body.Operation, body.Source)
		}
		return err
	})
	writeEditResult(w, result, err)
}

func handleAddOperationAlias(w http.ResponseWriter, r *http.Request) {
	handleOperationAliasChange(w, r, true)
}

func handleRemoveOperationAlias(w http.ResponseWriter, r *http.Request) {
	handleOperationAliasChange(w, r, false)
}

func handleOperationAliasChange(w http.ResponseWriter, r *http.Request, add bool) {
	var body struct {
		Interface *openbindings.Interface `json:"interface"`
		Operation string                  `json:"operation"`
		Aliases   []string                `json:"aliases"`
	}
	if !decodeRequest(w, r, &body) || !requireInterface(w, body.Interface) {
		return
	}
	result, err := editInterfaceFile(body.Interface, func(path string) error {
		if add {
			_, err := app.OperationAliasAdd(path, body.Operation, body.Aliases)
			return err
		}
		_, err := app.OperationAliasRemove(path, body.Operation, body.Aliases)
		return err
	})
	writeEditResult(w, result, err)
}

func handleListOperationAliases(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Interface *openbindings.Interface `json:"interface"`
		Operation string                  `json:"operation,omitempty"`
	}
	if !decodeRequest(w, r, &body) || !requireInterface(w, body.Interface) {
		return
	}
	var out app.OperationAliasListOutput
	_, err := editInterfaceFile(body.Interface, func(path string) error {
		var err error
		out, err = app.OperationAliasList(path, body.Operation)
		return err
	})
	if err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "list_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func handleSetOperationCodegenName(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Interface   *openbindings.Interface `json:"interface"`
		Operation   string                  `json:"operation"`
		CodegenName string                  `json:"codegenName,omitempty"`
		Clear       bool                    `json:"clear,omitempty"`
	}
	if !decodeRequest(w, r, &body) || !requireInterface(w, body.Interface) {
		return
	}
	if body.Clear {
		body.CodegenName = ""
	}
	result, err := editInterfaceFile(body.Interface, func(path string) error {
		_, err := app.OperationSetCodegenName(path, body.Operation, body.CodegenName)
		return err
	})
	writeEditResult(w, result, err)
}

func handleSetOperationOutputSchema(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Interface    *openbindings.Interface `json:"interface"`
		Operation    string                  `json:"operation"`
		OutputSchema map[string]any          `json:"outputSchema,omitempty"`
		Clear        bool                    `json:"clear,omitempty"`
	}
	if !decodeRequest(w, r, &body) || !requireInterface(w, body.Interface) {
		return
	}
	schema := openbindings.JSONSchema(body.OutputSchema)
	if body.Clear {
		schema = nil
	}
	result, err := editInterfaceFile(body.Interface, func(path string) error {
		_, err := app.OperationSetOutputSchema(path, body.Operation, schema)
		return err
	})
	writeEditResult(w, result, err)
}

func handleInitializeEnvironment(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Global bool `json:"global,omitempty"`
	}
	if !decodeRequest(w, r, &body) {
		return
	}
	status, err := app.Init(body.Global)
	if err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "initialization_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func handleRegisterDelegate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Location   string   `json:"location"`
		Preference *float64 `json:"preference,omitempty"`
	}
	if !decodeRequest(w, r, &body) {
		return
	}
	result, err := app.RegisterDelegate(body.Location, body.Preference)
	if err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "registration_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func handleUnregisterDelegate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Location string `json:"location"`
	}
	if !decodeRequest(w, r, &body) {
		return
	}
	if _, err := app.UnregisterDelegate(body.Location); err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "unregistration_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, nil)
}

func handleSetDelegatePreference(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Location    string          `json:"location"`
		Preference  json.RawMessage `json:"preference"`
		Operation   string          `json:"operation,omitempty"`
		BindingSpec string          `json:"bindingSpec,omitempty"`
	}
	if !decodeRequest(w, r, &body) {
		return
	}
	if body.Preference == nil {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "preference is required (use null to clear it)")
		return
	}
	var preference *float64
	if err := json.Unmarshal(body.Preference, &preference); err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "preference must be a number or null")
		return
	}
	result, err := app.SetDelegatePreference(app.SetDelegatePreferenceInput{
		Location: body.Location, Preference: preference, Operation: body.Operation, BindingSpec: body.BindingSpec,
	})
	if err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "preference_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func handleResolveDelegateForBindingSpec(w http.ResponseWriter, r *http.Request) {
	var body struct {
		BindingSpec string `json:"bindingSpec"`
	}
	if !decodeRequest(w, r, &body) {
		return
	}
	result, err := app.ResolveDelegateForBindingSpec(body.BindingSpec)
	if err != nil {
		writeErrorJSON(w, http.StatusNotFound, "delegate_not_found", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}
