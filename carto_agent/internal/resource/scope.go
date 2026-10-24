package resource

import "context"

type scopeContextKey struct{}
type trustedHandleContextKey struct{}
type resolverContextKey struct{}

func WithScope(ctx context.Context, scope Scope) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, scopeContextKey{}, scope)
}

func ScopeFromContext(ctx context.Context) (Scope, bool) {
	if ctx == nil {
		return Scope{}, false
	}
	scope, ok := ctx.Value(scopeContextKey{}).(Scope)
	return scope, ok
}

// WithInternalHandles marks arguments assembled by a trusted executor. Model
// tool arguments cannot opt into this by supplying fields in their JSON.
func WithInternalHandles(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, trustedHandleContextKey{}, true)
}

func InternalHandlesAllowed(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	allowed, _ := ctx.Value(trustedHandleContextKey{}).(bool)
	return allowed
}

func WithResolver(ctx context.Context, resolver Resolver) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, resolverContextKey{}, resolver)
}

func ResolverFromContext(ctx context.Context) Resolver {
	if ctx == nil {
		return nil
	}
	resolver, _ := ctx.Value(resolverContextKey{}).(Resolver)
	return resolver
}
