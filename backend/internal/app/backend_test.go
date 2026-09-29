package app

import "github.com/KitsuneSemCalda/OmaStore/backend/internal/rpc"

// *App must satisfy rpc.Backend.
var _ rpc.Backend = (*App)(nil)
