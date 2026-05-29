package authentik

import (
	"sort"
	"strings"
	"sync"
)

type PlatformNamespacePolicy struct {
	mu         sync.Mutex
	allowAll   bool
	configured map[string]struct{}
	firstSeen  string
}

func NewPlatformNamespacePolicy(raw string) *PlatformNamespacePolicy {
	policy := &PlatformNamespacePolicy{configured: map[string]struct{}{}}
	for _, item := range strings.Split(raw, ",") {
		namespace := strings.TrimSpace(item)
		if namespace == "" {
			continue
		}
		if namespace == "*" {
			policy.allowAll = true
			policy.configured = map[string]struct{}{}
			return policy
		}
		policy.configured[namespace] = struct{}{}
	}
	return policy
}

func (p *PlatformNamespacePolicy) Allow(namespace string) bool {
	if p == nil {
		return true
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.allowAll {
		return true
	}
	if len(p.configured) > 0 {
		_, ok := p.configured[namespace]
		return ok
	}
	if p.firstSeen == "" {
		p.firstSeen = namespace
		return true
	}
	return p.firstSeen == namespace
}

func (p *PlatformNamespacePolicy) Description() string {
	if p == nil {
		return "*"
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.allowAll {
		return "*"
	}
	if len(p.configured) == 0 {
		if p.firstSeen == "" {
			return "<first Tamoss auth platformNamespace>"
		}
		return p.firstSeen
	}
	namespaces := make([]string, 0, len(p.configured))
	for namespace := range p.configured {
		namespaces = append(namespaces, namespace)
	}
	sort.Strings(namespaces)
	return strings.Join(namespaces, ",")
}
