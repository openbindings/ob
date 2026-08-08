package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestOAuthGlyphPinsDesignIdentityRevision(t *testing.T) {
	const expected = "4b0f63a75b7e8f7805739b6a01706e8bc5f6b0ce86dfde4423c4c69551f5ec1e"

	digest := sha256.Sum256([]byte(oauthOpenBindingsGlyph))
	actual := hex.EncodeToString(digest[:])
	if actual != expected {
		t.Fatalf("OAuth glyph must match openbindings/design identity revision 1: got %s", actual)
	}
}
