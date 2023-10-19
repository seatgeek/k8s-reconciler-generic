package k8sutil

import (
	"context"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// SchemedClient simply binds a client and its associated scheme together since they're often both needed.
type SchemedClient struct {
	client.Client
	*runtime.Scheme
}

// ContextClient is a helper for a SchemedClient used in a specific context and namespace, i.e. during
// the course of a single invocation of Reconcile(), to make writing code that operates within this context
// less verbose.
type ContextClient struct {
	SchemedClient
	Context   context.Context
	Namespace string
}

func (r *ContextClient) Get(name string, obj client.Object) error {
	return r.Client.Get(r.Context, types.NamespacedName{
		Namespace: r.Namespace,
		Name:      name,
	}, obj)
}

func FetchInto[T client.Object](newT func() T, name string, r *ContextClient) (T, error) {
	obj := newT()
	if err := r.Get(name, obj); err != nil {
		var objNil T
		return objNil, client.IgnoreNotFound(err)
	}
	return obj, nil
}

func Fetch[T any, PT interface {
	*T
	client.Object
}](name string, r *ContextClient) (PT, error) {
	return FetchInto[PT](func() PT {
		return PT(new(T))
	}, name, r)
}
