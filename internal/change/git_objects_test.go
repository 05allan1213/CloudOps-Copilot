package change

import (
	"strings"
	"testing"
)

func TestExactGitObjectIdentityRejectsAbbreviationsAndBindsBlobBytes(t *testing.T) {
	if ValidExactGitObjectID("deadbeef") || ValidExactGitObjectID(strings.Repeat("A", 40)) {
		t.Fatal("abbreviated or uppercase Git object identity was accepted")
	}
	for _, length := range []int{40, 64} {
		objectID, err := GitBlobObjectID([]byte("healthy"), length)
		if err != nil || len(objectID) != length || !ValidExactGitObjectID(objectID) {
			t.Fatalf("blob object id length=%d id=%q err=%v", length, objectID, err)
		}
	}
}
