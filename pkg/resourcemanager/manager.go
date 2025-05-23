package resourcemanager

import (
	"errors"
	"k8s.io/apimachinery/pkg/types"
	"reflect"

	"github.com/seatgeek/k8s-reconciler-generic/pkg/genrec"
	"github.com/seatgeek/k8s-reconciler-generic/pkg/k8sutil"
	kmetav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ResourceManager is a helper utility to make it easier to implement correct ObserveResources and GenerateResources
// methods in your Logic[T]. You are free to use it, or to implement those by hand, or some mixture. But there is no
// dependency between the two, and you could in theory also use ResourceManager to implement observation and
// generation for non-generic controllers using vanilla kubebuilder.
//
// ResourceManager addresses the fact that Generate and Observe are very similar, in that they are referring to two
// sides of the same coin, and correctness dictates that the same resources you generate, you observe, and vice versa.
// But it can be tricky, particularly naming things, and if we know what GVK will be Generated, we should be able to
// implement Observe for you.
type ResourceManager[C Context] []ResourceHandler[C]

type Context interface {
	GetSubjectNamespace() string
	GetSubjectUID() types.UID
	GetClient() *k8sutil.ContextClient
	ObjectName(tier, suffix string) string
	ObjectMeta(tier, suffix string) kmetav1.ObjectMeta
}

// Only filters registered handlers by the tier, suffix provided
func (rm ResourceManager[C]) Only(tier, suffix string) ResourceManager[C] {
	var agg []ResourceHandler[C]
	for _, rh := range rm {
		if rh.Tier == tier && rh.Suffix == suffix {
			agg = append(agg, rh)
		}
	}
	return agg
}

func (rm ResourceManager[C]) RegisterOwnedTypes(cb *builder.Builder) error {
	for _, ex := range rm.UniqueExamplesByType() {
		cb.Owns(ex)
	}
	return nil
}

func (rm ResourceManager[C]) ObserveResources(c C) (genrec.Resources, error) {
	var err error
	res := make(genrec.Resources, 0, len(rm))
	for _, r := range rm {
		objName := c.ObjectName(r.Tier, r.Suffix)
		rec := genrec.Resource{ResourceOpts: r.ResourceOpts}
		if rec.Object, err = r.Observe(objName, c); err != nil {
			return nil, err
		}
		if genrec.IsNil(rec.Object) {
			continue
		}
		if rec.Key, err = r.resourceKey(objName, c); err != nil {
			return nil, err
		}
		res = append(res, rec)
	}
	return res, nil
}

type Observations struct {
	genrec.Resources
	SelfKey string
}

func (o *Observations) Self() client.Object {
	for _, resource := range o.Resources {
		if resource.Key == o.SelfKey {
			return resource.Object
		}
	}
	return nil
}

func (rm ResourceManager[C]) GenerateResources(c C) (genrec.Resources, error) {
	var err error
	res := make(genrec.Resources, 0, len(rm))
	for _, r := range rm {
		objMeta := c.ObjectMeta(r.Tier, r.Suffix)
		rec := genrec.Resource{ResourceOpts: r.ResourceOpts}
		if rec.Key, err = r.resourceKey(objMeta.Name, c); err != nil {
			return nil, err
		}
		if rec.Object, err = r.Generate(objMeta, c); err != nil {
			if errors.Is(err, genrec.ErrDoNothing) {
				rec.Pass = true
			} else {
				return nil, err
			}
		}
		if genrec.IsNil(rec.Object) && !rec.Pass {
			continue
		}
		res = append(res, rec)
	}
	return res, nil
}

func (rm ResourceManager[C]) UniqueExamplesByType() []client.Object {
	uniq := make(map[reflect.Type]client.Object, len(rm))
	for _, r := range rm {
		k := reflect.TypeOf(r.Example)
		if _, ok := uniq[k]; !ok {
			uniq[k] = r.Example
		}
	}
	res := make([]client.Object, 0, len(uniq))
	for _, ex := range uniq {
		res = append(res, ex)
	}
	return res
}

type Pred[C Context] func(C) bool
type Observe[C Context, T client.Object] func(name string, context C) (T, error)
type Generate[C Context, T client.Object] func(om kmetav1.ObjectMeta, context C) (T, error)

type GroupKindResolver func(sch *runtime.Scheme) (schema.GroupKind, error)

func makeGroupKindResolver(obj runtime.Object) GroupKindResolver {
	var cache *schema.GroupKind
	return func(sch *runtime.Scheme) (schema.GroupKind, error) {
		if cache == nil {
			gvks, _, err := sch.ObjectKinds(obj)
			if err != nil {
				return schema.GroupKind{}, err
			}
			gks := make(map[schema.GroupKind]struct{}, len(gvks))
			for _, gvk := range gvks {
				gks[gvk.GroupKind()] = struct{}{}
			}
			if len(gks) != 1 {
				return schema.GroupKind{}, errors.New("not exactly one groupkind for object")
			}
			for gk := range gks {
				gk := gk
				cache = &gk
			}
		}
		return *cache, nil
	}
}
