package bridgemap

import (
	"github.com/42wim/matterbridge/bridge"
)

var (
	FullMap           = map[string]bridge.Factory{}
	UserTypingSupport = map[string]struct{}{}
	// ReactionSupport lists protocols whose Send implements native emoji
	// reactions; populated per-protocol under the same build tag that compiles
	// the bridge in, so a bridge excluded via -tags can't claim the capability.
	ReactionSupport = map[string]struct{}{}
)
