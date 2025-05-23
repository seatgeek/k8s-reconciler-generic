package resourcemanager

import (
	"fmt"
	"github.com/seatgeek/k8s-reconciler-generic/pkg/genrec"
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

	HandlerOpts[C]
}

type HandlerOpts[C Context] struct {
	Requirements    []func(C) bool
	ClusterScoped   NameMapper[C]
	ObserveCallback func(client.Object, C)
	genrec.ResourceOpts
}

type NameMapper[C Context] func(context C, name string) string

func PrependNamespace[C Context](context C, name string) string {
	return fmt.Sprintf("%s-%s", context.GetSubjectNamespace(), name)
}

func AppendUID[C Context](context C, name string) string {
	return fmt.Sprintf("%s-%v", name, context.GetSubjectUID())
}

type OptsFunc[C Context] func(*HandlerOpts[C])

func ObserveCallback[C Context, T client.Object](cb func(T, C)) OptsFunc[C] {
	return func(opts *HandlerOpts[C]) {
		opts.ObserveCallback = func(object client.Object, c C) {
			if t, ok := object.(T); ok {
				cb(t, c)
			}
		}
	}
}

func Sensitive[C Context](opts *HandlerOpts[C]) {
	opts.IsSensitive = true
}

func DeleteOnChange[C Context](opts *HandlerOpts[C]) {
	opts.DeleteOnChange = true
}

func PatchUpdates[C Context](opts *HandlerOpts[C]) {
	opts.PatchUpdates = true
}

func Orphan[C Context](opts *HandlerOpts[C]) {
	opts.Orphan = true
}

func ClusterScoped[C Context](mapper NameMapper[C]) OptsFunc[C] {
	return func(opts *HandlerOpts[C]) {
		opts.Orphan = true
		opts.ClusterScoped = mapper
	}
}

func Requires[C Context](predicate func(C) bool) OptsFunc[C] {
	return func(opts *HandlerOpts[C]) {
		opts.Requirements = append(opts.Requirements, predicate)
	}
}

func NewHandler[T any, C Context, PT interface {
	*T
	client.Object
}](tier, suffix string, generator Generate[C, PT], ofs ...OptsFunc[C]) ResourceHandler[C] {
	var opts HandlerOpts[C]
	for _, of := range ofs {
		of(&opts)
	}

	newT := func() PT { return (PT)(new(T)) }
	var obsResolved Observe[C, client.Object]

	// we can implement Observe for you.
	obsResolved = func(name string, context C) (client.Object, error) {
		if opts.ClusterScoped != nil {
			name = opts.ClusterScoped(context, name)
		}
		obj, err := k8sutil.FetchInto(newT, name, context.GetClient())
		if err != nil {
			return nil, err
		}
		if opts.ObserveCallback != nil {
			opts.ObserveCallback(obj, context)
		}
		return obj, nil
	}

	example := client.Object(newT())
	return ResourceHandler[C]{
		Tier:    tier,
		Suffix:  suffix,
		Example: example,
		GetGK:   makeGroupKindResolver(example),
		Observe: obsResolved,
		Generate: func(om kmetav1.ObjectMeta, context C) (client.Object, error) {
			for _, requirement := range opts.Requirements {
				if !requirement(context) {
					return nil, nil
				}
			}
			if opts.ClusterScoped != nil {
				om.Name = opts.ClusterScoped(context, om.Name)
				om.Namespace = ""
			}
			return generator(om, context)
		},
		HandlerOpts: opts,
	}
}

func (r ResourceHandler[C]) resourceKey(objName string, c C) (string, error) {
	gk, err := r.GetGK(c.GetClient().Scheme)
	return RenderResourceKey(gk, objName), err
}

func RenderResourceKey(gk schema.GroupKind, objName string) string {
	return fmt.Sprintf("%s/%s", gk.String(), objName)
}
