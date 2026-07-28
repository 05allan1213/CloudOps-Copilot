package change

import (
	"crypto/sha1" // #nosec G505 -- Git SHA-1 object identity is a protocol requirement, not a security digest.
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// ValidExactGitObjectID accepts only complete lowercase SHA-1 or SHA-256 Git
// object identities. Abbreviated revisions remain valid for legacy browsing
// but cannot authorize delivery or remediation facts.
func ValidExactGitObjectID(value string) bool {
	value = strings.TrimSpace(value)
	if (len(value) != sha1.Size*2 && len(value) != sha256.Size*2) || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

// GitBlobObjectID computes the protocol object ID for an exact blob using the
// object format selected by the expected identity length.
func GitBlobObjectID(content []byte, objectIDLength int) (string, error) {
	payload := append([]byte(fmt.Sprintf("blob %d\x00", len(content))), content...)
	switch objectIDLength {
	case sha1.Size * 2:
		sum := sha1.Sum(payload) // #nosec G401 -- Git SHA-1 object identity is required for SHA-1 repositories.
		return hex.EncodeToString(sum[:]), nil
	case sha256.Size * 2:
		sum := sha256.Sum256(payload)
		return hex.EncodeToString(sum[:]), nil
	default:
		return "", ErrInvalidArgument
	}
}
