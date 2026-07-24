// Package servecontract owns the transport-level conventions shared by the
// ob start runtime and its generated API descriptions.
package servecontract

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sort"
)

// ErrorCode is a stable machine-readable error code returned by ob start.
type ErrorCode string

const (
	CodeCodegenFailed            ErrorCode = "codegen_failed"
	CodeComparisonFailed         ErrorCode = "comparison_failed"
	CodeContextStoreFailed       ErrorCode = "context_store_failed"
	CodeDelegateNotFound         ErrorCode = "delegate_not_found"
	CodeDelegateResolutionFailed ErrorCode = "delegate_resolution_failed"
	CodeEditFailed               ErrorCode = "edit_failed"
	CodeEnvironmentFailed        ErrorCode = "environment_failed"
	CodeInitializationFailed     ErrorCode = "initialization_failed"
	CodeInspectionFailed         ErrorCode = "inspection_failed"
	CodeInternal                 ErrorCode = "internal_error"
	CodeInvalidHost              ErrorCode = "invalid_host"
	CodeInvalidRequest           ErrorCode = "invalid_request"
	CodeInvalidUpstream          ErrorCode = "invalid_upstream"
	CodeListFailed               ErrorCode = "list_failed"
	CodeMergeFailed              ErrorCode = "merge_failed"
	CodeNotFound                 ErrorCode = "not_found"
	CodeOriginForbidden          ErrorCode = "origin_forbidden"
	CodePreferenceFailed         ErrorCode = "preference_failed"
	CodePreflightFailed          ErrorCode = "preflight_failed"
	CodePullFailed               ErrorCode = "pull_failed"
	CodeRegistrationFailed       ErrorCode = "registration_failed"
	CodeRelativeSourceReference  ErrorCode = "relative_source_reference"
	CodeRequestTooLarge          ErrorCode = "request_too_large"
	CodeResolutionFailed         ErrorCode = "resolution_failed"
	CodeResolutionForbidden      ErrorCode = "resolution_forbidden"
	CodeSourceExists             ErrorCode = "source_exists"
	CodeSourceFailed             ErrorCode = "source_failed"
	CodeStatusFailed             ErrorCode = "status_failed"
	CodeSynthesisFailed          ErrorCode = "synthesis_failed"
	CodeUnauthorized             ErrorCode = "unauthorized"
	CodeUnknownCapability        ErrorCode = "unknown_capability"
	CodeUnregistrationFailed     ErrorCode = "unregistration_failed"
	CodeUnsupportedLanguage      ErrorCode = "unsupported_language"
	CodeUnsupportedMediaType     ErrorCode = "unsupported_media_type"
	CodeUpgradeRequired          ErrorCode = "upgrade_required"
)

var errorCodeSet = map[ErrorCode]struct{}{
	CodeCodegenFailed: {}, CodeComparisonFailed: {}, CodeContextStoreFailed: {},
	CodeDelegateNotFound: {}, CodeDelegateResolutionFailed: {}, CodeEditFailed: {},
	CodeEnvironmentFailed: {}, CodeInitializationFailed: {}, CodeInspectionFailed: {},
	CodeInternal: {}, CodeInvalidHost: {}, CodeInvalidRequest: {}, CodeInvalidUpstream: {},
	CodeListFailed: {}, CodeMergeFailed: {}, CodeNotFound: {}, CodeOriginForbidden: {},
	CodePreferenceFailed: {}, CodePreflightFailed: {}, CodePullFailed: {},
	CodeRegistrationFailed: {}, CodeRelativeSourceReference: {}, CodeRequestTooLarge: {},
	CodeResolutionFailed: {}, CodeResolutionForbidden: {}, CodeSourceExists: {},
	CodeSourceFailed: {}, CodeStatusFailed: {}, CodeSynthesisFailed: {}, CodeUnauthorized: {},
	CodeUnknownCapability: {}, CodeUnregistrationFailed: {}, CodeUnsupportedLanguage: {},
	CodeUnsupportedMediaType: {}, CodeUpgradeRequired: {},
}

// ErrorResponse is the one JSON error envelope used by authenticated HTTP and
// WebSocket-upgrade endpoints. Error is prose; Code is the stable API.
type ErrorResponse struct {
	Error  string    `json:"error"`
	Code   ErrorCode `json:"code"`
	Detail string    `json:"detail,omitempty"`
}

// ErrorCodes returns the complete, sorted public vocabulary.
func ErrorCodes() []string {
	out := make([]string, 0, len(errorCodeSet))
	for code := range errorCodeSet {
		out = append(out, string(code))
	}
	sort.Strings(out)
	return out
}

// IsErrorCode reports whether code is part of the published vocabulary.
func IsErrorCode(code ErrorCode) bool {
	_, ok := errorCodeSet[code]
	return ok
}

// WriteError serializes the canonical error envelope. An undeclared code is a
// server defect, not a new public convention: fail closed with internal_error.
func WriteError(w http.ResponseWriter, status int, code ErrorCode, message string) {
	if !IsErrorCode(code) {
		slog.Error("undeclared ob start error code", "code", code, "status", status)
		status = http.StatusInternalServerError
		code = CodeInternal
		message = "internal server error"
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(ErrorResponse{Error: message, Code: code}); err != nil {
		slog.Error("write error response", "error", err)
	}
}
