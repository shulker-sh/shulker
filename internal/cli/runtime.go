package cli

import (
	"context"

	"shulker.sh/shulker/internal/java"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/project"
	"shulker.sh/shulker/internal/sync"
)

func (a *app) freshestJava(ctx context.Context, p *project.Project, fix out.Detail) (java.Runtime, error) {
	se, err := a.syncEnv()
	if err != nil {
		return java.Runtime{}, err
	}
	return sync.FreshestJava(ctx, se, p, fix)
}
