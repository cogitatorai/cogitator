package agent

import (
	"context"

	"github.com/cogitatorai/cogitator/server/internal/memory"
	"github.com/cogitatorai/cogitator/server/internal/provider"
)

// RetrieverAdapter wraps a memory.Retriever to satisfy the MemoryRetriever
// interface by formatting retrieved nodes into a string for the system prompt.
type RetrieverAdapter struct {
	Retriever    *memory.Retriever
	NameResolver memory.NameResolver
}

func (a *RetrieverAdapter) Retrieve(ctx context.Context, userID, message string, history []provider.Message) (string, error) {
	rc, err := a.Retriever.Retrieve(ctx, userID, message, history)
	if err != nil {
		return "", err
	}
	return rc.Format(a.NameResolver, userID), nil
}
