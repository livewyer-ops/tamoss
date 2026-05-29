package controller

import "strings"

func ParseWatchNamespaces(raw string) map[string]struct{} {
	namespaces := map[string]struct{}{}
	for _, item := range strings.Split(raw, ",") {
		namespace := strings.TrimSpace(item)
		if namespace == "" {
			continue
		}
		namespaces[namespace] = struct{}{}
	}
	if len(namespaces) == 0 {
		return nil
	}
	return namespaces
}
