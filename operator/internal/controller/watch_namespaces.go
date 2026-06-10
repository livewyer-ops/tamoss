package controller

import "strings"

// WatchNamespaceSet restricts reconciliation to an allow-list of namespaces.
// An empty set allows every namespace.
type WatchNamespaceSet map[string]struct{}

func (s WatchNamespaceSet) Allows(namespace string) bool {
	if len(s) == 0 {
		return true
	}
	_, ok := s[namespace]
	return ok
}

func ParseWatchNamespaces(raw string) WatchNamespaceSet {
	namespaces := WatchNamespaceSet{}
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
