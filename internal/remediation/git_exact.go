package remediation

import (
	"bytes"
	"context"
	"crypto/sha1" // #nosec G505 -- Git SHA-1 object identity is a protocol requirement, not a security digest.
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
	"sort"
	"strings"
	"unicode/utf8"
)

const MaxExactGitTreeEntries = 10000

// ExactGitRestoreQuery is constructed only from the frozen remediation policy
// and the active DeploymentBaseline. Async task payloads and model output never
// provide any of these Git query fields.
type ExactGitRestoreQuery struct {
	Repository       string
	BaseBranch       string
	TargetPath       string
	BaselineRevision string
}

type GitTreeEntry struct {
	Path     string
	Mode     string
	Type     string
	ObjectID string
}

// ExactGitRestoreFacts contains only bounded, read-only Git facts. TreeEntries
// is a complete recursive snapshot of BaseTreeSHA and must never be truncated.
type ExactGitRestoreFacts struct {
	Repository           string
	BaseBranch           string
	TargetPath           string
	BaseRevision         string
	BaseTreeSHA          string
	BaseBlobSHA          string
	FileMode             string
	CurrentContent       []byte
	BaselineRevision     string
	BaselineBlobSHA      string
	BaselineContent      []byte
	BaselineIsAncestor   bool
	BaseTreeWasTruncated bool
	TreeEntries          []GitTreeEntry
}

type ExactGitReader interface {
	ReadRestoreFacts(context.Context, ExactGitRestoreQuery) (ExactGitRestoreFacts, error)
}

// ValidateExactGitRestoreFacts verifies that provider-returned blob and tree
// identities are internally consistent before they can influence a Plan.
func ValidateExactGitRestoreFacts(facts ExactGitRestoreFacts) error {
	objectBytes, err := gitObjectBytes(facts.BaseTreeSHA)
	if err != nil || facts.BaseTreeWasTruncated || len(facts.TreeEntries) == 0 || len(facts.TreeEntries) > MaxExactGitTreeEntries {
		return fmt.Errorf("%w: exact Git tree identity is incomplete", ErrDrift)
	}
	objectHexLength := len(objectBytes) * 2
	for _, value := range []string{facts.BaseRevision, facts.BaseTreeSHA, facts.BaseBlobSHA, facts.BaselineRevision, facts.BaselineBlobSHA} {
		if len(value) != objectHexLength || strings.ToLower(value) != value {
			return fmt.Errorf("%w: Git object formats do not match", ErrDrift)
		}
		if _, err := gitObjectBytes(value); err != nil {
			return fmt.Errorf("%w: invalid Git object identity", ErrDrift)
		}
	}
	if strings.TrimSpace(facts.Repository) == "" || strings.TrimSpace(facts.BaseBranch) == "" ||
		!validGitTreePath(facts.TargetPath) || facts.FileMode != "100644" || !facts.BaselineIsAncestor ||
		len(facts.CurrentContent) == 0 || len(facts.CurrentContent) > MaxV3PostImageBytes ||
		len(facts.BaselineContent) == 0 || len(facts.BaselineContent) > MaxV3PostImageBytes {
		return fmt.Errorf("%w: exact Git restore facts are outside policy bounds", ErrDrift)
	}
	currentBlob, err := gitObjectHash("blob", facts.CurrentContent, objectHexLength)
	if err != nil || currentBlob != facts.BaseBlobSHA {
		return fmt.Errorf("%w: current Git blob content does not match its object ID", ErrDrift)
	}
	baselineBlob, err := gitObjectHash("blob", facts.BaselineContent, objectHexLength)
	if err != nil || baselineBlob != facts.BaselineBlobSHA {
		return fmt.Errorf("%w: baseline Git blob content does not match its object ID", ErrDrift)
	}
	root, target, err := computeGitTree(facts.TreeEntries, len(objectBytes), true, facts.TargetPath)
	if err != nil || root != facts.BaseTreeSHA || target == nil || target.ObjectID != facts.BaseBlobSHA || target.Mode != facts.FileMode || target.Type != "blob" {
		return fmt.Errorf("%w: recursive Git tree does not bind the target blob", ErrDrift)
	}
	return nil
}

// ExpectedGitTreeHash computes the exact Git tree object that would result
// from replacing the one allowlisted blob. It performs no Git write.
func ExpectedGitTreeHash(facts ExactGitRestoreFacts, postImage []byte) (string, error) {
	if err := ValidateExactGitRestoreFacts(facts); err != nil {
		return "", err
	}
	if len(postImage) == 0 || len(postImage) > MaxV3PostImageBytes {
		return "", fmt.Errorf("%w: post-image is outside Git blob bounds", ErrInvalidArgument)
	}
	objectHexLength := len(facts.BaseTreeSHA)
	postBlob, err := gitObjectHash("blob", postImage, objectHexLength)
	if err != nil {
		return "", err
	}
	entries := append([]GitTreeEntry(nil), facts.TreeEntries...)
	found := false
	for index := range entries {
		if entries[index].Path == facts.TargetPath {
			if entries[index].Mode != facts.FileMode || entries[index].Type != "blob" || entries[index].ObjectID != facts.BaseBlobSHA {
				return "", fmt.Errorf("%w: target blob changed inside the Git tree snapshot", ErrDrift)
			}
			entries[index].ObjectID = postBlob
			found = true
			break
		}
	}
	if !found {
		return "", fmt.Errorf("%w: target blob is absent from the Git tree", ErrDrift)
	}
	root, _, err := computeGitTree(entries, objectHexLength/2, false, facts.TargetPath)
	if err != nil {
		return "", err
	}
	return root, nil
}

func computeGitTree(entries []GitTreeEntry, objectBytes int, verifyExisting bool, targetPath string) (string, *GitTreeEntry, error) {
	if objectBytes != sha1.Size && objectBytes != sha256.Size {
		return "", nil, fmt.Errorf("%w: unsupported Git object format", ErrInvalidArgument)
	}
	objectHexLength := objectBytes * 2
	byPath := make(map[string]GitTreeEntry, len(entries))
	directChildren := make(map[string][]GitTreeEntry)
	directories := make(map[string]struct{})
	directories[""] = struct{}{}
	var target *GitTreeEntry
	for _, entry := range entries {
		entry.Path = strings.TrimSpace(entry.Path)
		entry.ObjectID = strings.ToLower(strings.TrimSpace(entry.ObjectID))
		if !validGitTreePath(entry.Path) || !validGitTreeEntry(entry, objectHexLength) {
			return "", nil, fmt.Errorf("%w: malformed Git tree entry", ErrDrift)
		}
		if _, duplicate := byPath[entry.Path]; duplicate {
			return "", nil, fmt.Errorf("%w: duplicate Git tree path", ErrDrift)
		}
		byPath[entry.Path] = entry
		if entry.Path == targetPath {
			copyEntry := entry
			target = &copyEntry
		}
		parent := path.Dir(entry.Path)
		if parent == "." {
			parent = ""
		}
		directChildren[parent] = append(directChildren[parent], entry)
		for current := parent; current != ""; current = parentGitPath(current) {
			directories[current] = struct{}{}
		}
		if entry.Type == "tree" {
			directories[entry.Path] = struct{}{}
		}
	}
	for directory := range directories {
		if directory == "" {
			continue
		}
		entry, ok := byPath[directory]
		if !ok || entry.Type != "tree" || entry.Mode != "040000" {
			return "", nil, fmt.Errorf("%w: recursive Git tree is missing a directory entry", ErrDrift)
		}
	}
	directoryList := make([]string, 0, len(directories))
	for directory := range directories {
		directoryList = append(directoryList, directory)
	}
	sort.Slice(directoryList, func(i, j int) bool {
		leftDepth, rightDepth := strings.Count(directoryList[i], "/"), strings.Count(directoryList[j], "/")
		if leftDepth == rightDepth {
			return directoryList[i] > directoryList[j]
		}
		return leftDepth > rightDepth
	})
	computed := make(map[string]string, len(directoryList))
	for _, directory := range directoryList {
		children := append([]GitTreeEntry(nil), directChildren[directory]...)
		if len(children) == 0 {
			return "", nil, fmt.Errorf("%w: empty or incomplete Git tree directory", ErrDrift)
		}
		sort.Slice(children, func(i, j int) bool { return gitTreeSortKey(children[i]) < gitTreeSortKey(children[j]) })
		var content bytes.Buffer
		for index := range children {
			child := children[index]
			objectID := child.ObjectID
			if child.Type == "tree" {
				calculated, ok := computed[child.Path]
				if !ok {
					return "", nil, fmt.Errorf("%w: nested Git tree was not computed", ErrDrift)
				}
				if verifyExisting && calculated != child.ObjectID {
					return "", nil, fmt.Errorf("%w: nested Git tree object does not match entries", ErrDrift)
				}
				objectID = calculated
			}
			rawObject, err := gitObjectBytes(objectID)
			if err != nil || len(rawObject) != objectBytes {
				return "", nil, fmt.Errorf("%w: Git tree child object is invalid", ErrDrift)
			}
			name := path.Base(child.Path)
			_, _ = content.WriteString(gitSerializedMode(child.Mode) + " " + name)
			_ = content.WriteByte(0)
			_, _ = content.Write(rawObject)
		}
		objectID, err := gitObjectHash("tree", content.Bytes(), objectHexLength)
		if err != nil {
			return "", nil, err
		}
		computed[directory] = objectID
	}
	root, ok := computed[""]
	if !ok {
		return "", nil, fmt.Errorf("%w: root Git tree was not computed", ErrDrift)
	}
	return root, target, nil
}

func validGitTreeEntry(entry GitTreeEntry, objectHexLength int) bool {
	if len(entry.ObjectID) != objectHexLength {
		return false
	}
	if _, err := gitObjectBytes(entry.ObjectID); err != nil {
		return false
	}
	switch entry.Type {
	case "tree":
		return entry.Mode == "040000"
	case "blob":
		return entry.Mode == "100644" || entry.Mode == "100755" || entry.Mode == "120000"
	case "commit":
		return entry.Mode == "160000"
	default:
		return false
	}
}

func validGitTreePath(value string) bool {
	return value != "" && utf8.ValidString(value) && !strings.ContainsRune(value, 0) && !strings.Contains(value, "\\") &&
		!strings.HasPrefix(value, "/") && !strings.HasSuffix(value, "/") && path.Clean(value) == value && value != "." && !strings.HasPrefix(value, "../")
}

func parentGitPath(value string) string {
	parent := path.Dir(value)
	if parent == "." {
		return ""
	}
	return parent
}

func gitTreeSortKey(entry GitTreeEntry) string {
	name := path.Base(entry.Path)
	if entry.Type == "tree" {
		return name + "/"
	}
	return name
}

func gitSerializedMode(mode string) string {
	if mode == "040000" {
		return "40000"
	}
	return mode
}

func gitObjectHash(kind string, content []byte, objectHexLength int) (string, error) {
	header := []byte(fmt.Sprintf("%s %d\x00", kind, len(content)))
	payload := make([]byte, 0, len(header)+len(content))
	payload = append(payload, header...)
	payload = append(payload, content...)
	switch objectHexLength {
	case sha1.Size * 2:
		sum := sha1.Sum(payload) // #nosec G401 -- Git SHA-1 object identity is required for GitHub SHA-1 repositories.
		return hex.EncodeToString(sum[:]), nil
	case sha256.Size * 2:
		sum := sha256.Sum256(payload)
		return hex.EncodeToString(sum[:]), nil
	default:
		return "", fmt.Errorf("%w: unsupported Git object hash length", ErrInvalidArgument)
	}
}

func gitObjectBytes(value string) ([]byte, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if len(value) != sha1.Size*2 && len(value) != sha256.Size*2 {
		return nil, fmt.Errorf("%w: unsupported Git object ID", ErrInvalidArgument)
	}
	decoded, err := hex.DecodeString(value)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid Git object ID", ErrInvalidArgument)
	}
	return decoded, nil
}
