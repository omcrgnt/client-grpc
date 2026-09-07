package clientgrpc

import (
	"context"

	"github.com/omcrgnt/app"
	"google.golang.org/grpc"
)

// Tagged wraps Client behind a phantom type parameter, letting one service
// hold several outbound gRPC clients to different targets at once. The
// org-framework's DI registry is type-unique (at most one entry per
// concrete Go type — see github.com/omcrgnt/res/unique's doc.go): two plain
// *Client catalog fields in the same resources struct collide at startup
// ("entry for type already has TagRegular"), no matter the distinct ecfg
// tag on each field — the tag only routes env vars into the pre-Build
// Config, it doesn't survive into the registry once both specs materialize
// into the same *Client type. Tagged[UserTag] and Tagged[CharacterTag] are
// distinct Go types even though structurally identical, so the registry
// holds both without collision — same trick srv-grpc's Server[T]/srv-http's
// Server[T] already use for handler types.
//
// Usage: define an empty tag type per named client, e.g.
//
//	type UserTag struct{}
//	UserGRPC *clientgrpc.Tagged[UserTag] `ecfg:"USER"`
//
// For a tagged client that also needs WithUnaryClientInterceptors (e.g.
// gctxgrpc propagation onward to a compat-filtering store — see
// npc-dialogue's shopstore client), use NewTagged/WithTaggedUnaryClientInterceptors
// below, same non-nil-catalog-field rule as Client's own New.
type Tagged[Tag any] struct {
	inner *Client

	// extraUnary carries NewTagged's options through to BuildConfig ->
	// taggedConfig.Build — unexported, so ecfg's reflection-based walker
	// can't set it and leaves it alone (same reasoning as Client.extraUnary).
	extraUnary []grpc.UnaryClientInterceptor
}

// TaggedOption configures a Tagged[Tag] at construction time — same purpose
// as Client's own Option, for values ecfg can't fill.
type TaggedOption[Tag any] func(*Tagged[Tag])

// WithTaggedUnaryClientInterceptors is Tagged's equivalent of
// WithUnaryClientInterceptors.
func WithTaggedUnaryClientInterceptors[Tag any](in ...grpc.UnaryClientInterceptor) TaggedOption[Tag] {
	return func(t *Tagged[Tag]) { t.extraUnary = append(t.extraUnary, in...) }
}

// NewTagged constructs a Tagged[Tag] with the given options applied. The
// catalog field holding it must be assigned this (non-nil) — same
// catalogCallable rule as Client's own New (see its doc comment).
func NewTagged[Tag any](opts ...TaggedOption[Tag]) *Tagged[Tag] {
	t := &Tagged[Tag]{}
	for _, opt := range opts {
		opt(t)
	}
	return t
}

// taggedConfig redefines Config as a new named (generic) type — same field
// layout (so ecfg fills Label/Host/Port identically), but a clean method
// set: Config's own Build isn't inherited, letting taggedConfig define its
// own that returns *Tagged[Tag] instead of *Client.
type taggedConfig[Tag any] Config

func (s *taggedConfig[Tag]) Build() (any, error) {
	// Pointer conversion, not a value copy: taggedConfig[Tag]'s underlying
	// type is identical to Config's, and a value copy here would trip go
	// vet's copylocks check — common.Label embeds a protobuf MessageState
	// containing sync-like internals.
	v, err := (*Config)(s).Build()
	if err != nil {
		return nil, err
	}
	return &Tagged[Tag]{inner: v.(*Client)}, nil
}

var _ app.Configurable = (*Tagged[struct{}])(nil)

func (t *Tagged[Tag]) BuildConfig() (app.Materializer, error) {
	return &taggedConfig[Tag]{extraUnary: t.extraUnary}, nil
}

// Deps/Inject/StandBy/Conn/Label/Target below are plain passthroughs to the
// inner *Client — see client.go for the actual dial/metrics lifecycle.

func (c *Tagged[Tag]) Deps() []any {
	return c.inner.Deps()
}

func (c *Tagged[Tag]) Inject(args []any) {
	c.inner.Inject(args)
}

func (c *Tagged[Tag]) StandBy() (func(context.Context) error, error) {
	return c.inner.StandBy()
}

func (c *Tagged[Tag]) Conn() *grpc.ClientConn {
	return c.inner.Conn()
}

func (c *Tagged[Tag]) Label() string {
	if c == nil {
		return ""
	}
	return c.inner.Label()
}

func (c *Tagged[Tag]) Target() string {
	if c == nil {
		return ""
	}
	return c.inner.Target()
}
