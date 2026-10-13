package service

import (
	"context"
	"errors"
)

type organizationNonBillableContextKey struct{}
type organizationNonBillableProviderRequestIDContextKey struct{}

// WithOrganizationNonBillableCountTokens disables compatibility fallbacks
// that would turn a provider dispatch failure into an unobservable local token
// estimate. Organization evidence must reflect the actual dispatch outcome.
func WithOrganizationNonBillableCountTokens(ctx context.Context) context.Context {
	return context.WithValue(ctx, organizationNonBillableContextKey{}, true)
}

func WithOrganizationNonBillableProviderRequestID(ctx context.Context, requestID string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, organizationNonBillableProviderRequestIDContextKey{}, requestID)
}

func isOrganizationNonBillableCountTokens(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	value, _ := ctx.Value(organizationNonBillableContextKey{}).(bool)
	return value
}

func organizationNonBillableProviderRequestID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	value, _ := ctx.Value(organizationNonBillableProviderRequestIDContextKey{}).(string)
	return value
}

type upstreamNoHTTPResponseError struct {
	cause error
}

type upstreamPreDispatchError struct {
	cause error
}

type upstreamOutcomeUnknownError struct {
	cause error
}

func (e *upstreamOutcomeUnknownError) Error() string {
	if e == nil || e.cause == nil {
		return "upstream dispatch outcome is unknown"
	}
	return e.cause.Error()
}

func (e *upstreamOutcomeUnknownError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func (e *upstreamPreDispatchError) Error() string {
	if e == nil || e.cause == nil {
		return "upstream request was rejected before dispatch"
	}
	return e.cause.Error()
}

func (e *upstreamPreDispatchError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func (e *upstreamNoHTTPResponseError) Error() string {
	if e == nil || e.cause == nil {
		return "upstream request failed before an HTTP response"
	}
	return e.cause.Error()
}

func (e *upstreamNoHTTPResponseError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func newUpstreamNoHTTPResponseError(err error) error {
	if err == nil {
		return nil
	}
	return &upstreamNoHTTPResponseError{cause: err}
}

func newUpstreamPreDispatchError(err error) error {
	if err == nil {
		return nil
	}
	return &upstreamPreDispatchError{cause: err}
}

func organizationCountTokensPreDispatchError(ctx context.Context, err error) error {
	if !isOrganizationNonBillableCountTokens(ctx) {
		return err
	}
	return newUpstreamPreDispatchError(err)
}

func organizationCountTokensOutcomeUnknownError(ctx context.Context, err error) error {
	if !isOrganizationNonBillableCountTokens(ctx) {
		return err
	}
	if err == nil {
		return nil
	}
	return newUpstreamOutcomeUnknownError(err)
}

func newUpstreamOutcomeUnknownError(err error) error {
	if err == nil {
		return nil
	}
	return &upstreamOutcomeUnknownError{cause: err}
}

// IsUpstreamNoHTTPResponse distinguishes an attempted provider dispatch that
// produced no HTTP response from local request-construction failures.
func IsUpstreamNoHTTPResponse(err error) bool {
	var target *upstreamNoHTTPResponseError
	return errors.As(err, &target)
}

// IsUpstreamPreDispatchError identifies local request construction or
// credential errors that must not create provider attempt evidence.
func IsUpstreamPreDispatchError(err error) bool {
	var target *upstreamPreDispatchError
	return errors.As(err, &target)
}

// IsUpstreamOutcomeUnknown identifies a completed dispatch whose provider
// response could not be observed completely enough to create final evidence.
func IsUpstreamOutcomeUnknown(err error) bool {
	var target *upstreamOutcomeUnknownError
	return errors.As(err, &target)
}
