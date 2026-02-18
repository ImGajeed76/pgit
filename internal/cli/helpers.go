package cli

import (
	"context"
	"fmt"

	"github.com/imgajeed76/pgit/v3/internal/repo"
	"github.com/imgajeed76/pgit/v3/internal/util"
)

// connectForCommand connects to either the local or remote database.
// If remoteName is empty, connects to the local container database.
// If remoteName is set, connects directly to the named remote and verifies schema exists.
// The returned repository's DB field points to the chosen database.
// Caller must defer r.Close().
func connectForCommand(ctx context.Context, remoteName string) (*repo.Repository, error) {
	r, err := repo.Open()
	if err != nil {
		return nil, err
	}

	if remoteName == "" {
		if err := r.Connect(ctx); err != nil {
			return nil, err
		}
		return r, nil
	}

	// Remote connection
	remote, exists := r.Config.GetRemote(remoteName)
	if !exists {
		return nil, util.RemoteNotFoundError(remoteName)
	}

	remoteDB, err := r.ConnectTo(ctx, remote.URL)
	if err != nil {
		return nil, util.DatabaseConnectionError(remote.URL, err)
	}

	// Verify schema exists on remote
	schemaExists, err := remoteDB.SchemaExists(ctx)
	if err != nil {
		remoteDB.Close()
		return nil, err
	}
	if !schemaExists {
		remoteDB.Close()
		return nil, util.NewError("Remote database has no pgit schema").
			WithMessage(fmt.Sprintf("The remote '%s' exists but has no pgit data", remoteName)).
			WithSuggestion(fmt.Sprintf("pgit push %s  # Push your repository first", remoteName))
	}

	// Swap DB to point at remote
	r.DB = remoteDB
	return r, nil
}
