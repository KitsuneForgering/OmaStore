package app

import "github.com/KitsuneSemCalda/OmaStore/backend/internal/rpc"

// *App precisa satisfazer rpc.Backend.
var _ rpc.Backend = (*App)(nil)
