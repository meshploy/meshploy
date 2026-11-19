package service

import (
	"context"
	"log"
	"time"

	appk8s "github.com/meshploy/packages/server/k8s"
)

// keepBuilderEvery is how often every build node is checked for the builder:
// often enough that a node which joined, rebooted or had it removed has it
// back before most builds, and cheap, since a node that has it pulls nothing.
const keepBuilderEvery = time.Hour

// KeepBuilders keeps the builder downloaded and pinned on every build node,
// so a build never starts by downloading it: shortly after the API starts,
// which is when an upgrade has brought a new builder, and every hour after.
// Returns when ctx ends.
func (s *NodeService) KeepBuilders(ctx context.Context) {
	if s.cluster == nil {
		return
	}
	t := time.NewTimer(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		s.keepBuilders(ctx)
		t.Reset(keepBuilderEvery)
	}
}

func (s *NodeService) keepBuilders(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	nodes, err := appk8s.BuildNodes(ctx, s.k8s)
	if err != nil {
		log.Printf("keep the builder: list build nodes: %v", err)
		return
	}
	for _, n := range nodes {
		s.keepBuilderOn(ctx, n)
	}
}

// keepBuilderOn starts keeping the builder on one node, logging what came of
// it; a node already downloading it is left to finish.
func (s *NodeService) keepBuilderOn(ctx context.Context, node string) {
	started, err := s.cluster.KeepBuilder(ctx, node, s.builderImage)
	switch {
	case err != nil:
		log.Printf("keep the builder on %s: %v", node, err)
	case started:
		log.Printf("keeping the builder on %s", node)
	}
}
