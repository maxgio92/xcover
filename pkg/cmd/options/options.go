package options

import (
	"context"

	log "github.com/rs/zerolog"
)

type CommonOptions struct {
	Ctx    context.Context
	Logger log.Logger
}
