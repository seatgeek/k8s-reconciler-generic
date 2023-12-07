package resourcemanager

import (
	"fmt"
	"github.com/seatgeek/k8s-reconciler-generic/pkg/k8sutil"
	kmetav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ResourceHandler[C Context] struct {
	Tier   string
	Suffix string

	Example  client.Object
	GetGK    GroupKindResolver
	Observe  Observe[C, client.Object]
	Generate Generate[C, client.Object]

	IsSensitive    bool
	DeleteOnChange bool
}

func NewHandler[T any, C Context, PT interface {
	*T
	client.Object
}](tier, suffix string, generator Generate[C, PT]) ResourceHandler[C] {
	newT := func() PT { return (PT)(new(T)) }
	var obsResolved Observe[C, client.Object]

	// we can implement Observe for you.
	obsResolved = func(name string, context C) (client.Object, error) {
		return k8sutil.FetchInto(newT, name, context.GetClient())
	}

	example := client.Object(newT())
	return ResourceHandler[C]{
		Tier:    tier,
		Suffix:  suffix,
		Example: example,
		GetGK:   makeGroupKindResolver(example),
		Observe: obsResolved,
		Generate: func(om kmetav1.ObjectMeta, context C) (client.Object, error) {
			return generator(om, context)
		},
	}
}

func (r ResourceHandler[C]) WithIsSensitive(v bool) ResourceHandler[C] {
	r.IsSensitive = v
	return r
}

func (r ResourceHandler[C]) WithDeleteOnChange(v bool) ResourceHandler[C] {
	r.DeleteOnChange = v
	return r
}

func (r ResourceHandler[C]) resourceKey(objName string, c C) (string, error) {
	gk, err := r.GetGK(c.GetClient().Scheme)
	return RenderResourceKey(gk, objName), err
}

func RenderResourceKey(gk schema.GroupKind, objName string) string {
	return fmt.Sprintf("%s/%s", gk.String(), objName)
}
