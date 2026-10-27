package filecontext

import (
	"context"
	"errors"
	"time"
)

type budgetKey struct{}

func sourceFailure(err error, fallback Code) error {
	var typed *Error
	if errors.As(err, &typed) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return fail(fallback)
}

func (s *Service) Ready() bool             { return s != nil && s.source != nil }
func (s *Service) MessageReferences() bool { return s.Ready() }
func (s *Service) WorkspaceBrowser() bool  { return s.Ready() && s.workspaceBrowser }

func budgeted[T any](ctx context.Context, s *Service, call func(context.Context) (T, error)) (T, error) {
	var zero T
	if !s.Ready() {
		return zero, fail(CodeFeatureDisabled)
	}
	if ctx.Value(budgetKey{}) == s {
		return call(ctx)
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	select {
	case s.waiting <- struct{}{}:
	default:
		return zero, fail(CodeOverloaded)
	}
	defer func() { <-s.waiting }()
	select {
	case s.slots <- struct{}{}:
	case <-ctx.Done():
		return zero, fail(CodeTimeout)
	}
	defer func() { <-s.slots }()
	result, err := call(context.WithValue(ctx, budgetKey{}, s))
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return zero, fail(CodeTimeout)
	}
	return result, err
}

func (s *Service) Changes(ctx context.Context, req ChangesRequest) (ChangesResponse, error) {
	return budgeted(ctx, s, func(ctx context.Context) (ChangesResponse, error) { return s.changes(ctx, req) })
}
func (s *Service) Read(ctx context.Context, req ReadRequest) (ReadResponse, error) {
	return budgeted(ctx, s, func(ctx context.Context) (ReadResponse, error) { return s.read(ctx, req) })
}
func (s *Service) ReferenceRead(ctx context.Context, req ReferenceReadRequest) (ReadResponse, error) {
	return budgeted(ctx, s, func(ctx context.Context) (ReadResponse, error) { return s.referenceRead(ctx, req) })
}
func (s *Service) WorkspaceList(ctx context.Context, req WorkspaceListRequest) (WorkspaceListResponse, error) {
	return budgeted(ctx, s, func(ctx context.Context) (WorkspaceListResponse, error) { return s.workspaceList(ctx, req) })
}
func (s *Service) WorkspaceRead(ctx context.Context, req WorkspaceReadRequest) (ReadResponse, error) {
	return budgeted(ctx, s, func(ctx context.Context) (ReadResponse, error) { return s.workspaceRead(ctx, req) })
}
func (s *Service) Prepare(ctx context.Context, req PrepareRequest) (PrepareResponse, error) {
	return budgeted(ctx, s, func(ctx context.Context) (PrepareResponse, error) { return s.prepare(ctx, req) })
}
