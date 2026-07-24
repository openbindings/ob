package servecontract

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestErrorCodesAreSortedUnique(t *testing.T) {
	codes := ErrorCodes()
	if len(codes) != len(errorCodeSet) {
		t.Fatalf("ErrorCodes returned %d codes for a %d-code catalog", len(codes), len(errorCodeSet))
	}
	for i, code := range codes {
		if i > 0 && codes[i-1] >= code {
			t.Fatalf("codes are not strictly sorted at %q, %q", codes[i-1], code)
		}
		if !IsErrorCode(ErrorCode(code)) {
			t.Fatalf("%q is not recognized by its own catalog", code)
		}
	}
}

func TestWriteError(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteError(rec, http.StatusBadRequest, CodeInvalidRequest, "bad input")

	var body ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	want := ErrorResponse{Error: "bad input", Code: CodeInvalidRequest}
	if rec.Code != http.StatusBadRequest || !reflect.DeepEqual(body, want) {
		t.Fatalf("response = %d %#v, want 400 %#v", rec.Code, body, want)
	}
}

func TestWriteErrorRefusesUndeclaredCode(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteError(rec, http.StatusBadRequest, ErrorCode("new_code_by_accident"), "leaked detail")

	var body ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusInternalServerError || body.Code != CodeInternal || body.Error != "internal server error" {
		t.Fatalf("response = %d %#v, want a closed internal_error", rec.Code, body)
	}
}
