// Package util provides small helpers shared across the k8s integration test
// framework (harness, deps).
package util

import (
	"fmt"
	"regexp"
	"sort"
)

// placeholderRE matches ${UPPER_CASE_NAME} placeholders. Bare $FOO is
// intentionally not recognized so this is safe to apply to YAML/JSON content
// where lone dollar signs may appear in unrelated strings.
var placeholderRE = regexp.MustCompile(`\$\{([A-Z_][A-Z0-9_]*)\}`)

// SubstituteVars replaces every ${KEY} occurrence in content with vars[KEY].
// It returns an error if any ${KEY} has no entry in vars, listing the missing
// keys (deduplicated and sorted) so typos and missing values fail loudly.
func SubstituteVars(content string, vars map[string]string) (string, error) {
	var unresolved []string
	out := placeholderRE.ReplaceAllStringFunc(content, func(match string) string {
		key := match[2 : len(match)-1]
		if v, ok := vars[key]; ok {
			return v
		}
		unresolved = append(unresolved, key)
		return match
	})
	if len(unresolved) > 0 {
		seen := make(map[string]struct{}, len(unresolved))
		unique := unresolved[:0]
		for _, k := range unresolved {
			if _, ok := seen[k]; ok {
				continue
			}
			seen[k] = struct{}{}
			unique = append(unique, k)
		}
		sort.Strings(unique)
		return "", fmt.Errorf("unresolved ${VAR} placeholders: %v", unique)
	}
	return out, nil
}
