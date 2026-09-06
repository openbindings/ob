package cmd

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/openbindings/ob/internal/app"
	"github.com/openbindings/ob/internal/server"
)

// registerCanonicalOperationRoutes makes the app-level route inventories
// executable. OpenAPI, AsyncAPI, the served OBI, coverage tests, and the
// runtime mux therefore share one operation-to-route declaration.
func registerCanonicalOperationRoutes(srv *server.Server, logger *slog.Logger) {
	httpHandlers := map[string]http.HandlerFunc{
		"describe":                      handleDescribe,
		"listBindingSpecs":              handleBindingSpecs,
		"checkBindingSpecs":             handleCheckBindingSpecs,
		"resolveInterface":              handleResolve,
		"synthesizeInterface":           handleInterfaceSynthesize,
		"inspectSource":                 handleSourceInspect,
		"addSource":                     handleAddSource,
		"removeSource":                  handleRemoveSource,
		"listSources":                   handleListSources,
		"listBindings":                  handleListBindings,
		"addOperation":                  handleAddOperation,
		"renameOperation":               handleRenameOperation,
		"removeOperation":               handleRemoveOperation,
		"listOperations":                handleListOperations,
		"mergeInterfaces":               handleMerge,
		"validateInterface":             handleValidate,
		"compareInterfaces":             handleDiff,
		"reportCompatibility":           handleCompat,
		"prepareBinding":                handleBindingPrepare(logger),
		"prepareOperation":              handleOperationPrepare(logger),
		"getContext":                    handleContextGet,
		"setContext":                    handleContextSet,
		"removeContext":                 handleContextDelete,
		"listContexts":                  handleContextList,
		"registerDelegate":              handleRegisterDelegate,
		"unregisterDelegate":            handleUnregisterDelegate,
		"listDelegates":                 handleDelegates,
		"resolveDelegate":               handleResolveDelegate,
		"resolveDelegateForBindingSpec": handleResolveDelegateForBindingSpec,
		"getDelegateRequirements":       handleDelegateRequirements,
		"setDelegatePreference":         handleSetDelegatePreference,
		"initializeEnvironment":         handleInitializeEnvironment,
		"reportEnvironmentStatus":       handleEnvironment,
		"reportInterfaceStatus":         handleInterfaceStatus,
		"codegen":                       handleCodegen,
		"conform":                       handleConform,
		"addOperationAlias":             handleAddOperationAlias,
		"removeOperationAlias":          handleRemoveOperationAlias,
		"listOperationAliases":          handleListOperationAliases,
		"pullSource":                    handlePullSource,
		"purifyInterface":               handlePurifyInterface,
		"setOperation":                  handleSetOperation,
		"detachOperation":               handleDetachOperation,
		"setOperationCodegenName":       handleSetOperationCodegenName,
		"setOperationOutputSchema":      handleSetOperationOutputSchema,
		"bindOperation":                 handleBindOperation,
		"unbindOperation":               handleUnbindOperation,
		"newInterface":                  handleNewInterface,
		"setMetadata":                   handleSetMetadata,
	}

	mux := srv.Mux()
	diagnostics := &workbenchDiagnostics{}
	mux.HandleFunc("GET /workbench/diagnostics/{id}", diagnostics.serve)
	registered := make(map[string]bool, len(httpHandlers))
	for _, route := range app.ServeHTTPRoutes() {
		handler := httpHandlers[route.Operation]
		if handler == nil {
			panic(fmt.Sprintf("canonical serve route %s has no handler", route.Operation))
		}
		path := route.Path
		if route.RuntimePath != "" {
			path = route.RuntimePath
		}
		mux.HandleFunc(strings.ToUpper(route.Method)+" "+path, handler)
		registered[route.Operation] = true
	}
	for operation := range httpHandlers {
		if !registered[operation] {
			panic(fmt.Sprintf("serve handler %s has no canonical route", operation))
		}
	}

	streamHandlers := map[string]http.HandlerFunc{
		"invokeBinding":   handleBindingInvoke(srv, logger),
		"invokeOperation": handleOperationInvoke(srv, logger, diagnostics),
	}
	for _, route := range app.ServeStreamRoutes() {
		handler := streamHandlers[route.Operation]
		if handler == nil {
			panic(fmt.Sprintf("canonical stream route %s has no handler", route.Operation))
		}
		mux.HandleFunc("GET "+route.Path, handler)
		delete(streamHandlers, route.Operation)
	}
	for operation := range streamHandlers {
		panic(fmt.Sprintf("stream handler %s has no canonical route", operation))
	}
}
