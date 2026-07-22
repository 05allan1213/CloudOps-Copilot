// Package gitopscontract validates the fixed external Demo GitOps manifest tree.
package gitopscontract

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

const (
	maxBytes    = 65_536
	maxASTNodes = 20_000
	namespace   = "demo"
)

var (
	inventory = map[string]struct {
		kind string
		name string
	}{
		"deployment.yaml":          {kind: "Deployment", name: "demo"},
		"diagnostics-service.yaml": {kind: "Service", name: "demo-diagnostics"},
		"podmonitor.yaml":          {kind: "PodMonitor", name: "demo"},
		"prometheusrule.yaml":      {kind: "PrometheusRule", name: "demo"},
		"service.yaml":             {kind: "Service", name: "demo"},
	}
	apiVersions = map[string]string{
		"Deployment":     "apps/v1",
		"Service":        "v1",
		"PodMonitor":     "monitoring.coreos.com/v1",
		"PrometheusRule": "monitoring.coreos.com/v1",
	}
	requiredDownwardEnv = map[string]string{
		"K8S_NAMESPACE": "metadata.namespace",
		"K8S_POD_NAME":  "metadata.name",
		"K8S_POD_UID":   "metadata.uid",
	}
	dnsLabelPattern = regexp.MustCompile(`^[a-z0-9](?:[-a-z0-9]*[a-z0-9])?$`)
	imagePattern    = regexp.MustCompile(`^[a-z0-9._:/-]+@sha256:[0-9a-f]{64}$`)
	envNamePattern  = regexp.MustCompile(`^[A-Z_][A-Z0-9_]{0,127}$`)
	deniedKeys      = map[string]struct{}{
		"ephemeralContainers": {}, "envFrom": {}, "hostIPC": {}, "hostNetwork": {},
		"hostPID": {}, "hostPath": {}, "hostPort": {}, "initContainers": {},
		"privileged": {}, "imagePullSecrets": {}, "secret": {}, "imagePullSecret": {},
		"secretKeyRef": {}, "secretRef": {}, "serviceAccountName": {},
		"persistentVolumeClaim": {}, "projected": {}, "configMapKeyRef": {},
		"configMapRef": {}, "downwardAPI": {}, "serviceAccountToken": {},
	}
)

// ValidateHealthy requires the exact five-file external inventory and one
// literal REQUIRED_ENV entry in the Deployment.
func ValidateHealthy(directory string) error {
	_, err := loadTree(directory, 1)
	return err
}

// ValidateRegression requires a second exact five-file tree whose only
// semantic difference is removal of REQUIRED_ENV from the Deployment.
func ValidateRegression(healthyDirectory, regressionDirectory string) error {
	healthy, err := loadTree(healthyDirectory, 1)
	if err != nil {
		return err
	}
	regression, err := loadTree(regressionDirectory, 0)
	if err != nil {
		return err
	}
	removeRequiredEnv(healthy["deployment.yaml"])
	if !reflect.DeepEqual(healthy, regression) {
		return errors.New("regression tree changes fields or files other than REQUIRED_ENV removal")
	}
	return nil
}

func loadTree(directory string, requiredEnvCount int) (map[string]map[string]any, error) {
	root, err := filepath.Abs(directory)
	if err != nil {
		return nil, fmt.Errorf("resolve manifest directory: %w", err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("read manifest directory %s: %w", root, err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	expected := make([]string, 0, len(inventory))
	for name := range inventory {
		expected = append(expected, name)
	}
	slices.Sort(names)
	slices.Sort(expected)
	if !slices.Equal(names, expected) {
		return nil, fmt.Errorf("%s inventory is incomplete or contains extra paths", root)
	}

	objects := make(map[string]map[string]any, len(entries))
	for _, name := range names {
		path := filepath.Join(root, name)
		object, err := loadDocument(path)
		if err != nil {
			return nil, err
		}
		if err := validateObject(object, path, name); err != nil {
			return nil, err
		}
		objects[name] = object
	}
	deployment := objects["deployment.yaml"]
	count, err := requiredEnvCountIn(deployment)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Join(root, "deployment.yaml"), err)
	}
	if count != requiredEnvCount {
		return nil, fmt.Errorf("%s must contain exactly %d REQUIRED_ENV literal(s)", filepath.Join(root, "deployment.yaml"), requiredEnvCount)
	}
	return objects, nil
}

func loadDocument(path string) (map[string]any, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", path, err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("%s must be a regular non-symlink file", path)
	}
	if info.Size() > maxBytes {
		return nil, fmt.Errorf("%s exceeds 64 KiB", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if bytes.IndexByte(data, 0) >= 0 {
		return nil, fmt.Errorf("%s contains a NUL byte", path)
	}

	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var document yaml.Node
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("%s is invalid YAML: %w", path, err)
	}
	if len(document.Content) != 1 || document.Content[0] == nil {
		return nil, fmt.Errorf("%s has an empty YAML document", path)
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); err == nil {
		return nil, fmt.Errorf("%s must contain exactly one YAML document", path)
	} else if !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%s is invalid YAML: %w", path, err)
	}

	count := 0
	if err := inspectNode(document.Content[0], path, &count); err != nil {
		return nil, err
	}
	if document.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("%s root must be a mapping", path)
	}
	var object map[string]any
	if err := document.Content[0].Decode(&object); err != nil {
		return nil, fmt.Errorf("%s cannot decode into a string-keyed mapping: %w", path, err)
	}
	return object, nil
}

func inspectNode(node *yaml.Node, path string, count *int) error {
	*count++
	if *count > maxASTNodes {
		return fmt.Errorf("%s exceeds the YAML complexity limit", path)
	}
	if node.Kind == yaml.AliasNode {
		return fmt.Errorf("%s contains a YAML alias", path)
	}
	if node.Anchor != "" {
		return fmt.Errorf("%s contains a YAML anchor", path)
	}
	if node.Style&yaml.TaggedStyle != 0 {
		return fmt.Errorf("%s contains an explicit YAML tag", path)
	}
	if node.Kind == yaml.MappingNode {
		if len(node.Content)%2 != 0 {
			return fmt.Errorf("%s has an invalid mapping", path)
		}
		keys := make(map[string]struct{}, len(node.Content)/2)
		for index := 0; index < len(node.Content); index += 2 {
			key := node.Content[index]
			if key.Kind != yaml.ScalarNode {
				return fmt.Errorf("%s contains a non-scalar mapping key", path)
			}
			if key.Value == "<<" {
				return fmt.Errorf("%s contains a YAML merge key", path)
			}
			if _, exists := keys[key.Value]; exists {
				return fmt.Errorf("%s contains duplicate key %q", path, key.Value)
			}
			keys[key.Value] = struct{}{}
		}
	}
	for _, child := range node.Content {
		if err := inspectNode(child, path, count); err != nil {
			return err
		}
	}
	return nil
}

func validateObject(object map[string]any, path, inventoryName string) error {
	if extra := keysOutside(object, "apiVersion", "kind", "metadata", "spec"); len(extra) > 0 {
		return fmt.Errorf("%s contains denied top-level fields: %s", path, strings.Join(extra, ", "))
	}
	expected, ok := inventory[inventoryName]
	if !ok {
		return fmt.Errorf("%s is outside the fixed rendered inventory", path)
	}
	kind, ok := object["kind"].(string)
	if !ok {
		return fmt.Errorf("%s kind is not a string", path)
	}
	apiVersion, ok := apiVersions[kind]
	if !ok {
		return fmt.Errorf("%s kind is not allowed: %q", path, kind)
	}
	if kind != expected.kind {
		return fmt.Errorf("%s kind must be %s", path, expected.kind)
	}
	if object["apiVersion"] != apiVersion {
		return fmt.Errorf("%s apiVersion must be %s", path, apiVersion)
	}
	metadata, ok := asMap(object["metadata"])
	if !ok {
		return fmt.Errorf("%s metadata must be a mapping", path)
	}
	if err := validateMetadata(metadata, path, expected.name); err != nil {
		return err
	}
	if _, ok := asMap(object["spec"]); !ok {
		return fmt.Errorf("%s spec must be a mapping", path)
	}
	if err := walk(object, "$", 0, func(key, location string) error {
		if _, denied := deniedKeys[key]; denied {
			return fmt.Errorf("%s contains denied field %s at %s", path, key, location)
		}
		return nil
	}); err != nil {
		return err
	}

	switch kind {
	case "Deployment":
		return validateDeployment(object, path)
	case "Service":
		return validateService(object, path)
	case "PodMonitor":
		labels, _ := asMap(metadata["labels"])
		if labels["cloudops.io/monitoring"] != "enabled" {
			return fmt.Errorf("%s must opt in to the fixed monitoring selector", path)
		}
		expectedSelector := map[string]any{"matchLabels": map[string]any{"app.kubernetes.io/name": "demo"}}
		if !reflect.DeepEqual(dig(object, "spec", "selector"), expectedSelector) {
			return fmt.Errorf("%s PodMonitor selector must target only demo", path)
		}
		namespaceSelector := dig(object, "spec", "namespaceSelector")
		if namespaceSelector != nil && !reflect.DeepEqual(namespaceSelector, map[string]any{"matchNames": []any{"demo"}}) {
			return fmt.Errorf("%s PodMonitor namespaceSelector must remain in demo", path)
		}
	case "PrometheusRule":
		labels, _ := asMap(metadata["labels"])
		if labels["cloudops.io/monitoring"] != "enabled" {
			return fmt.Errorf("%s must opt in to the fixed monitoring selector", path)
		}
	}
	return nil
}

func validateMetadata(metadata map[string]any, path, expectedName string) error {
	if extra := keysOutside(metadata, "name", "namespace", "labels", "annotations"); len(extra) > 0 {
		return fmt.Errorf("%s metadata contains denied fields: %s", path, strings.Join(extra, ", "))
	}
	name, nameOK := metadata["name"].(string)
	ns, namespaceOK := metadata["namespace"].(string)
	if !nameOK || len(name) > 63 || !dnsLabelPattern.MatchString(name) {
		return fmt.Errorf("%s metadata.name is invalid", path)
	}
	if !namespaceOK || len(ns) > 63 || !dnsLabelPattern.MatchString(ns) {
		return fmt.Errorf("%s metadata.namespace is invalid", path)
	}
	if name != expectedName {
		return fmt.Errorf("%s metadata.name must be %s", path, expectedName)
	}
	if ns != namespace {
		return fmt.Errorf("%s metadata.namespace must be %s", path, namespace)
	}
	for _, key := range []string{"labels", "annotations"} {
		value, exists := metadata[key]
		if !exists {
			continue
		}
		mapping, ok := asMap(value)
		if !ok {
			return fmt.Errorf("%s metadata.%s must be a mapping", path, key)
		}
		for itemKey, item := range mapping {
			text, ok := item.(string)
			if !ok {
				return fmt.Errorf("%s metadata.%s.%s must be a string", path, key, itemKey)
			}
			if len(text) > 512 {
				return fmt.Errorf("%s metadata.%s.%s is too large", path, key, itemKey)
			}
		}
	}
	annotations, _ := asMap(metadata["annotations"])
	for key := range annotations {
		if strings.HasPrefix(key, "argocd.argoproj.io/hook") || strings.HasPrefix(key, "helm.sh/hook") {
			return fmt.Errorf("%s contains a denied deployment hook annotation", path)
		}
	}
	return nil
}

func validateDeployment(object map[string]any, path string) error {
	container, podSpec, err := deploymentContainer(object)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if dig(object, "spec", "replicas") != 2 {
		return fmt.Errorf("%s Deployment must run exactly two replicas", path)
	}
	if container["name"] != "demo" {
		return fmt.Errorf("%s Deployment container.name must be demo", path)
	}
	image, ok := container["image"].(string)
	if !ok || !imagePattern.MatchString(image) {
		return fmt.Errorf("%s Deployment container.image must be an immutable digest", path)
	}
	if err := validateSecurityContexts(podSpec, container, path); err != nil {
		return err
	}
	selector := map[string]any{"app.kubernetes.io/name": "demo"}
	if !reflect.DeepEqual(dig(object, "spec", "selector"), map[string]any{"matchLabels": selector}) {
		return fmt.Errorf("%s Deployment selector must be fixed", path)
	}
	templateLabels, ok := asMap(dig(object, "spec", "template", "metadata", "labels"))
	if !ok || templateLabels["app.kubernetes.io/name"] != "demo" {
		return fmt.Errorf("%s Deployment pod labels must match the fixed selector", path)
	}

	volumes := []any{}
	if value, exists := podSpec["volumes"]; exists {
		volumes, ok = asSlice(value)
		if !ok {
			return fmt.Errorf("%s Deployment volumes must be an array", path)
		}
	}
	volumeNames := make(map[string]struct{}, len(volumes))
	for _, value := range volumes {
		volume, ok := asMap(value)
		if !ok {
			return fmt.Errorf("%s Deployment volume must be a mapping", path)
		}
		name, ok := volume["name"].(string)
		if !ok || !dnsLabelPattern.MatchString(name) {
			return fmt.Errorf("%s Deployment volume names must be DNS labels", path)
		}
		if _, exists := volumeNames[name]; exists {
			return fmt.Errorf("%s Deployment volume names must be unique", path)
		}
		volumeNames[name] = struct{}{}
		if !hasOnlyKeys(volume, "emptyDir", "name") || !reflect.DeepEqual(volume["emptyDir"], map[string]any{}) {
			return fmt.Errorf("%s only emptyDir volumes with empty mappings are allowed", path)
		}
	}

	mounts := []any{}
	if value, exists := container["volumeMounts"]; exists {
		mounts, ok = asSlice(value)
		if !ok {
			return fmt.Errorf("%s volumeMounts must be an array", path)
		}
	}
	for _, value := range mounts {
		mount, ok := asMap(value)
		if !ok {
			return fmt.Errorf("%s volumeMount must be a mapping", path)
		}
		if len(keysOutside(mount, "mountPath", "name", "readOnly")) > 0 {
			return fmt.Errorf("%s volumeMount contains denied fields", path)
		}
		if mount["mountPath"] != "/tmp" || mount["name"] != "tmp" {
			return fmt.Errorf("%s volumeMount may only mount /tmp", path)
		}
		if mount["readOnly"] == true {
			return fmt.Errorf("%s /tmp volumeMount must be writable", path)
		}
	}
	if len(mounts) > 0 {
		if _, exists := volumeNames["tmp"]; !exists {
			return fmt.Errorf("%s /tmp volume must be declared", path)
		}
	}
	return validateEnv(container, path)
}

func validateSecurityContexts(podSpec, container map[string]any, path string) error {
	if podSpec["automountServiceAccountToken"] != false {
		return fmt.Errorf("%s automountServiceAccountToken must be false", path)
	}
	podContext, ok := asMap(podSpec["securityContext"])
	if !ok {
		return fmt.Errorf("%s pod securityContext must be a mapping", path)
	}
	if extra := keysOutside(podContext, "fsGroup", "runAsGroup", "runAsNonRoot", "runAsUser", "seccompProfile"); len(extra) > 0 {
		return fmt.Errorf("%s pod securityContext has denied fields", path)
	}
	if podContext["runAsNonRoot"] != true {
		return fmt.Errorf("%s pod runAsNonRoot must be true", path)
	}
	for _, key := range []string{"fsGroup", "runAsGroup", "runAsUser"} {
		if value, exists := podContext[key]; exists && !positiveInteger(value) {
			return fmt.Errorf("%s pod %s must be a positive integer", path, key)
		}
	}
	if !reflect.DeepEqual(podContext["seccompProfile"], map[string]any{"type": "RuntimeDefault"}) {
		return fmt.Errorf("%s pod seccompProfile must be RuntimeDefault", path)
	}

	containerContext, ok := asMap(container["securityContext"])
	if !ok {
		return fmt.Errorf("%s container securityContext must be a mapping", path)
	}
	if extra := keysOutside(containerContext, "allowPrivilegeEscalation", "capabilities", "readOnlyRootFilesystem", "runAsGroup", "runAsNonRoot", "runAsUser", "seccompProfile"); len(extra) > 0 {
		return fmt.Errorf("%s container securityContext has denied fields", path)
	}
	if containerContext["allowPrivilegeEscalation"] != false {
		return fmt.Errorf("%s allowPrivilegeEscalation must be false", path)
	}
	if containerContext["readOnlyRootFilesystem"] != true {
		return fmt.Errorf("%s readOnlyRootFilesystem must be true", path)
	}
	if containerContext["runAsNonRoot"] != true {
		return fmt.Errorf("%s container runAsNonRoot must be true", path)
	}
	for _, key := range []string{"runAsGroup", "runAsUser"} {
		if value, exists := containerContext[key]; exists && !positiveInteger(value) {
			return fmt.Errorf("%s container %s must be a positive integer", path, key)
		}
	}
	if !reflect.DeepEqual(containerContext["seccompProfile"], map[string]any{"type": "RuntimeDefault"}) {
		return fmt.Errorf("%s container seccompProfile must be RuntimeDefault", path)
	}
	if !reflect.DeepEqual(containerContext["capabilities"], map[string]any{"drop": []any{"ALL"}}) {
		return fmt.Errorf("%s container capabilities must drop ALL", path)
	}
	return nil
}

func validateEnv(container map[string]any, path string) error {
	env := []any{}
	if value, exists := container["env"]; exists {
		var ok bool
		env, ok = asSlice(value)
		if !ok {
			return fmt.Errorf("%s Deployment env must be an array", path)
		}
	}
	names := make(map[string]struct{}, len(env))
	for index, value := range env {
		entry, ok := asMap(value)
		if !ok {
			return fmt.Errorf("%s env[%d] must be a mapping", path, index)
		}
		name, ok := entry["name"].(string)
		if !ok || !envNamePattern.MatchString(name) {
			return fmt.Errorf("%s env[%d].name is invalid", path, index)
		}
		if _, exists := names[name]; exists {
			return fmt.Errorf("%s has duplicate env name %s", path, name)
		}
		names[name] = struct{}{}
		if value, literal := entry["value"]; literal {
			if !hasOnlyKeys(entry, "name", "value") {
				return fmt.Errorf("%s env %s must contain only name and value", path, name)
			}
			text, ok := value.(string)
			if !ok || text == "" || len(text) > 512 || strings.ContainsAny(text, "\x00\r\n") {
				return fmt.Errorf("%s env %s must be a bounded literal string", path, name)
			}
			continue
		}
		expectedField, allowed := requiredDownwardEnv[name]
		if !allowed || !hasOnlyKeys(entry, "name", "valueFrom") {
			return fmt.Errorf("%s env %s may not use valueFrom", path, name)
		}
		expected := map[string]any{"fieldRef": map[string]any{"apiVersion": "v1", "fieldPath": expectedField}}
		if !reflect.DeepEqual(entry["valueFrom"], expected) {
			return fmt.Errorf("%s env %s has an unapproved fieldRef", path, name)
		}
	}
	for name, fieldPath := range requiredDownwardEnv {
		expected := map[string]any{
			"name": name,
			"valueFrom": map[string]any{
				"fieldRef": map[string]any{"apiVersion": "v1", "fieldPath": fieldPath},
			},
		}
		found := false
		for _, value := range env {
			entry, _ := asMap(value)
			if reflect.DeepEqual(entry, expected) {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("%s must inject %s through the Downward API", path, name)
		}
	}
	return nil
}

func validateService(object map[string]any, path string) error {
	spec, _ := asMap(object["spec"])
	if serviceType, exists := spec["type"]; exists && serviceType != "ClusterIP" {
		return fmt.Errorf("%s Service must be ClusterIP", path)
	}
	if !reflect.DeepEqual(spec["selector"], map[string]any{"app.kubernetes.io/name": "demo"}) {
		return fmt.Errorf("%s Service selector must target only demo", path)
	}
	for _, key := range []string{"externalName", "externalIPs", "loadBalancerIP", "loadBalancerSourceRanges", "healthCheckNodePort"} {
		if _, exists := spec[key]; exists {
			return fmt.Errorf("%s Service contains denied field %s", path, key)
		}
	}
	if value, exists := spec["ports"]; exists {
		ports, ok := asSlice(value)
		if !ok {
			return fmt.Errorf("%s Service ports must be an array", path)
		}
		for _, value := range ports {
			port, ok := asMap(value)
			if !ok {
				return fmt.Errorf("%s Service ports must be mappings", path)
			}
			if _, exists := port["nodePort"]; exists {
				return fmt.Errorf("%s Service may not allocate nodePort", path)
			}
		}
	}
	diagnostics := dig(object, "metadata", "name") == "demo-diagnostics"
	if diagnostics && spec["publishNotReadyAddresses"] != true {
		return fmt.Errorf("%s diagnostics Service must publish not-ready addresses", path)
	}
	if !diagnostics && spec["publishNotReadyAddresses"] == true {
		return fmt.Errorf("%s normal Service may not publish not-ready addresses", path)
	}
	return nil
}

func deploymentContainer(object map[string]any) (map[string]any, map[string]any, error) {
	podSpec, ok := asMap(dig(object, "spec", "template", "spec"))
	if !ok {
		return nil, nil, errors.New("Deployment pod spec must be a mapping")
	}
	containers, ok := asSlice(podSpec["containers"])
	if !ok || len(containers) != 1 {
		return nil, nil, errors.New("Deployment must contain exactly one container")
	}
	container, ok := asMap(containers[0])
	if !ok {
		return nil, nil, errors.New("Deployment container must be a mapping")
	}
	return container, podSpec, nil
}

func requiredEnvCountIn(object map[string]any) (int, error) {
	container, _, err := deploymentContainer(object)
	if err != nil {
		return 0, err
	}
	env, ok := asSlice(container["env"])
	if !ok {
		return 0, nil
	}
	count := 0
	for _, value := range env {
		entry, _ := asMap(value)
		if entry["name"] == "REQUIRED_ENV" {
			count++
		}
	}
	return count, nil
}

func removeRequiredEnv(object map[string]any) {
	container, _, err := deploymentContainer(object)
	if err != nil {
		return
	}
	env, ok := asSlice(container["env"])
	if !ok {
		return
	}
	filtered := make([]any, 0, len(env))
	for _, value := range env {
		entry, _ := asMap(value)
		if entry["name"] != "REQUIRED_ENV" {
			filtered = append(filtered, value)
		}
	}
	if len(filtered) == 0 {
		delete(container, "env")
		return
	}
	container["env"] = filtered
}

func walk(value any, path string, depth int, visit func(key, location string) error) error {
	if depth > 40 {
		return fmt.Errorf("%s exceeds the nesting limit", path)
	}
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			if err := visit(key, path); err != nil {
				return err
			}
			if err := walk(item, path+"."+key, depth+1, visit); err != nil {
				return err
			}
		}
	case []any:
		for index, item := range typed {
			if err := walk(item, fmt.Sprintf("%s[%d]", path, index), depth+1, visit); err != nil {
				return err
			}
		}
	case string:
		if len(typed) > 16_384 {
			return fmt.Errorf("%s contains an oversized scalar", path)
		}
	case float64:
		if math.IsInf(typed, 0) || math.IsNaN(typed) {
			return fmt.Errorf("%s contains a non-finite number", path)
		}
	case int, int64, uint64, bool, nil:
	default:
		return fmt.Errorf("%s contains an unsupported YAML type %T", path, value)
	}
	return nil
}

func dig(root map[string]any, keys ...string) any {
	var current any = root
	for _, key := range keys {
		mapping, ok := asMap(current)
		if !ok {
			return nil
		}
		current = mapping[key]
	}
	return current
}

func asMap(value any) (map[string]any, bool) {
	mapping, ok := value.(map[string]any)
	return mapping, ok
}

func asSlice(value any) ([]any, bool) {
	items, ok := value.([]any)
	return items, ok
}

func keysOutside(mapping map[string]any, allowed ...string) []string {
	set := make(map[string]struct{}, len(allowed))
	for _, key := range allowed {
		set[key] = struct{}{}
	}
	extra := make([]string, 0)
	for key := range mapping {
		if _, ok := set[key]; !ok {
			extra = append(extra, key)
		}
	}
	slices.Sort(extra)
	return extra
}

func hasOnlyKeys(mapping map[string]any, expected ...string) bool {
	return len(mapping) == len(expected) && len(keysOutside(mapping, expected...)) == 0
}

func positiveInteger(value any) bool {
	switch typed := value.(type) {
	case int:
		return typed > 0
	case int64:
		return typed > 0
	case uint64:
		return typed > 0
	default:
		return false
	}
}
