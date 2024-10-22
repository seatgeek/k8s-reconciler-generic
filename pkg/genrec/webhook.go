package genrec

import (
	"context"
	"fmt"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

type validatingWebhookShim[S Subject, C any] struct {
	Logic[S, C]
}

func (v *validatingWebhookShim[S, C]) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	return v.validate(obj)
}

func (v *validatingWebhookShim[S, C]) ValidateUpdate(_ context.Context, _, newObj runtime.Object) (admission.Warnings, error) {
	return v.validate(newObj)
}

func (v *validatingWebhookShim[S, C]) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	// not currently configurable
	return nil, nil
}

func (v *validatingWebhookShim[S, C]) validate(obj runtime.Object) (admission.Warnings, error) {
	subj, ok := obj.(S)
	if !ok {
		return nil, fmt.Errorf("validating webhook: expected a %T but got a %T", v.Logic.NewSubject(), subj)
	}
	if err := v.Validate(subj); err != nil {
		return nil, err
	}
	return nil, nil
}

type mutatingWebhookShim[S Subject, C any] struct {
	*Reconciler[S, C]
	MutatingWebhookLogic[S, C]
}

func (m *mutatingWebhookShim[S, C]) Default(ctx context.Context, obj runtime.Object) error {
	req, err := admission.RequestFromContext(ctx)
	if err != nil {
		return fmt.Errorf("mutating webhook: could not find request details: %w", err)
	}
	subj, ok := obj.(S)
	if !ok {
		return fmt.Errorf("mutating webhook: expected a %T but got a %T", m.Logic.NewSubject(), subj)
	}
	rctx := m.newContext(ctx, types.NamespacedName{
		Namespace: subj.GetNamespace(),
		Name:      subj.GetName(),
	})
	rctx.Subject = subj // in your Mutate method, you transform this object in-place
	return m.MutatingWebhookLogic.Mutate(rctx, req)
}

type MutatingWebhookLogic[S Subject, C any] interface {
	Mutate(*Context[S, C], admission.Request) error
}
