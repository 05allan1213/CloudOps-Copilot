package k8s

import (
	"context"
	"errors"
	"fmt"

	copilotk8s "server-web/internal/copilot/k8s"
)

var errPromClientNotConfigured = errors.New("prometheus client is not configured")

func (s *Service) NodeHostAssociation(ctx context.Context, nodeNames []string) (map[string]HostAssociation, error) {
	if s.promClient == nil {
		return nil, errPromClientNotConfigured
	}
	ctx, cancel := s.withTimeout(ctx)
	defer cancel()

	hosts, err := s.promClient.GetHosts(ctx)
	if err != nil {
		return nil, fmt.Errorf("query host metrics for association: %w", err)
	}
	hostMap := make(map[string]HostInfo, len(hosts))
	for _, h := range hosts {
		hostMap[h.Instance] = h
	}
	result := make(map[string]HostAssociation, len(nodeNames))
	for _, name := range nodeNames {
		if h, ok := hostMap[name]; ok {
			result[name] = HostAssociation{Online: h.Status == "up", LastScrape: h.LastScrape}
		} else {
			result[name] = HostAssociation{Online: false}
		}
	}
	return result, nil
}

func (s *Service) FindNodeByInstance(ctx context.Context, instance string) (*copilotk8s.NodeSummary, error) {
	if !s.nodesEnabled {
		return nil, ErrNodesNotEnabled
	}
	ctx, cancel := s.withTimeout(ctx)
	defer cancel()

	nodes, err := s.reader.ListNodes(ctx, copilotk8s.QueryOptions{Limit: MaxLimit})
	if err != nil {
		return nil, fmt.Errorf("list nodes for instance lookup: %w", err)
	}
	for i := range nodes {
		if nodes[i].Name == instance {
			return &nodes[i], nil
		}
	}
	return nil, nil
}
