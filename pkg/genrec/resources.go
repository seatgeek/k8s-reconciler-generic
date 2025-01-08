package genrec

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	om "github.com/banzaicloud/k8s-objectmatcher/patch"
	"github.com/go-logr/logr"
	"github.com/seatgeek/k8s-reconciler-generic/pkg/k8sutil"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
)

// ErrDoNothing is an error that can be returned from a Generate function if you want
// to indicate explicitly that no resources should be created or deleted, and implies
// the related genrec.Resource.Pass = true
var ErrDoNothing = fmt.Errorf("just sit back and relax, its your day off")

type Resources []Resource

type ResourceMap map[string]*Resource

//goland:noinspection GoMixedReceiverTypes
func (r Resources) ToMap() ResourceMap {
	res := make(map[string]*Resource, len(r))
	for i := range r {
		res[r[i].Key] = &(r[i])
	}
	return res
}

//goland:noinspection GoMixedReceiverTypes
func (r *Resources) Add(res Resource, err error) error {
	if err != nil {
		return err
	}
	*r = append(*r, res)
	return nil
}

//goland:noinspection GoMixedReceiverTypes
func (r *Resources) Append(res Resources, err error) error {
	if err != nil {
		return err
	}
	*r = append(*r, res...)
	return nil
}

type ResourceOpts struct {
	IsSensitive    bool
	DeleteOnChange bool
	Orphan         bool
}

type Resource struct {
	// Key is a unique name assigned to this resource inside the controller flow for tracking and diffing it
	Key string
	// Object is the actual object instance, should never be nil (do not return a Resource at all for a nil object)
	Object client.Object
	// Pass indicates whether the resource should be ignored for diffs, it is set when ErrDoNothing is returned from a generator.
	Pass bool
	// ResourceOpts holds options controlling reconciliation behavior for this resource
	ResourceOpts
}

func Observe[T client.Object](key, name string, observer func(string, *k8sutil.ContextClient) (T, error), f *k8sutil.ContextClient) (Resource, error) {
	obj, err := observer(name, f)
	if err != nil {
		return Resource{}, err
	}
	return Resource{
		Key:    key,
		Object: obj,
	}, nil
}

type ResourceDiff struct {
	Key      string
	Observed client.Object
	Desired  client.Object
	Pass     bool
	ResourceOpts
}

type ResourceDiffOp string

const (
	ResourceDiffOpNone   ResourceDiffOp = ""
	ResourceDiffOpUpdate ResourceDiffOp = "UPDATE"
	ResourceDiffOpCreate ResourceDiffOp = "CREATE"
	ResourceDiffOpDelete ResourceDiffOp = "DELETE"
)

func (rd ResourceDiff) Op() ResourceDiffOp {
	if rd.Pass {
		return ResourceDiffOpNone
	}
	if o, d := rd.IsObserved(), rd.IsDesired(); o && d {
		return ResourceDiffOpUpdate
	} else if o {
		return ResourceDiffOpDelete
	} else if d {
		return ResourceDiffOpCreate
	} else {
		return ResourceDiffOpNone
	}
}

func (rd ResourceDiff) IsObserved() bool {
	return !IsNil(rd.Observed)
}

func (rd ResourceDiff) IsDesired() bool {
	return !IsNil(rd.Desired)
}

func (rd ResourceDiff) Exemplar() client.Object {
	if rd.IsDesired() {
		return rd.Desired
	}
	return rd.Observed
}

type ResourceDiffs []ResourceDiff

func (rd ResourceDiffs) ToMap() map[string]*ResourceDiff {
	res := make(map[string]*ResourceDiff, len(rd))
	for i := range rd {
		res[rd[i].Key] = &(rd[i])
	}
	return res
}

func IsNil(o client.Object) bool {
	return o == nil || reflect.ValueOf(o).IsNil()
}

func DiffResources(observed, desired Resources) ResourceDiffs {
	res := make([]ResourceDiff, 0, len(desired)+len(observed))
	desMap, obsMap := desired.ToMap(), observed.ToMap()

	// prepend the deletes (observed but not desired)
	for _, obs := range observed {
		if desMap[obs.Key] == nil {
			res = append(res, ResourceDiff{
				Key:      obs.Key,
				Observed: obs.Object,
			})
		}
	}

	for _, des := range desired {
		rd := ResourceDiff{
			Key:          des.Key,
			Desired:      des.Object,
			Pass:         des.Pass,
			ResourceOpts: des.ResourceOpts,
		}
		if obs := obsMap[des.Key]; obs != nil {
			rd.Observed = obs.Object
		}
		res = append(res, rd)
	}

	return res
}

func (rd ResourceDiffs) Issues(issuesFunc func(object client.Object) []string) []string {
	issues := make([]string, 0, len(rd))
	for _, diff := range rd {
		var facts []string
		isObserved, isDesired := diff.IsObserved(), diff.IsDesired()
		if isObserved && isDesired {
			facts = append(facts, issuesFunc(diff.Observed)...)
		} else if isObserved {
			facts = append(facts, "unwanted")
		} else if isDesired {
			facts = append(facts, "missing")
		}
		if len(facts) > 0 {
			issues = append(issues, fmt.Sprintf("%s(%s)", diff.Key, strings.Join(facts, ",")))
		}
	}
	return issues
}

var calcOptions = []om.CalculateOption{
	om.IgnoreStatusFields(),
	om.IgnoreVolumeClaimTemplateTypeMetaAndStatus(),
	ignorePDBSelector,
}

func (rd ResourceDiff) Apply(ctx context.Context, sc k8sutil.SchemedClient, owner client.Object) error {
	op := rd.Op()
	if op == ResourceDiffOpNone {
		// quick exit, both desired and observed are nil
		return nil
	}
	log := logr.FromContextOrDiscard(ctx).WithValues("objOp", op, "obj", rd.logInfoMap(sc))
	switch op {
	case ResourceDiffOpUpdate:
		if patchResult, err := om.DefaultPatchMaker.Calculate(rd.Observed, rd.Desired, calcOptions...); err != nil {
			return err
		} else if patchResult.IsEmpty() {
			log.V(2).Info("object is up-to-date")
			return nil
		} else if rd.DeleteOnChange {
			log.Info("deleting changed object")
			return client.IgnoreNotFound(sc.Delete(ctx, rd.Observed, client.PropagationPolicy(v1.DeletePropagationBackground)))
		} else {
			if rd.IsSensitive {
				log.Info("updating sensitive object; diff hidden")
			} else {
				log.Info("updating object", "patch", string(patchResult.Patch))
			}
			if err = om.DefaultAnnotator.SetLastAppliedAnnotation(rd.Desired); err != nil {
				return err
			}
			if owner != nil && !rd.Orphan {
				if err = ctrl.SetControllerReference(owner, rd.Desired, sc.Scheme); err != nil {
					return err
				}
			}
			rd.Desired.SetResourceVersion(rd.Observed.GetResourceVersion())
			return sc.Update(ctx, rd.Desired)
		}
	case ResourceDiffOpCreate:
		log.Info("creating missing object")
		if err := om.DefaultAnnotator.SetLastAppliedAnnotation(rd.Desired); err != nil {
			return err
		}
		if owner != nil && !rd.Orphan {
			if err := ctrl.SetControllerReference(owner, rd.Desired, sc.Scheme); err != nil {
				return err
			}
		}
		return sc.Create(ctx, rd.Desired)
	case ResourceDiffOpDelete:
		log.Info("deleting unwanted object")
		return client.IgnoreNotFound(sc.Delete(ctx, rd.Observed, client.PropagationPolicy(v1.DeletePropagationBackground)))
	default:
		log.Info("no-op")
		return nil
	}
}

func (rd ResourceDiff) logInfoMap(sc k8sutil.SchemedClient) map[string]interface{} {
	exemplar := rd.Exemplar()
	if IsNil(exemplar) {
		return nil
	}
	infoMap := map[string]interface{}{
		"namespace": exemplar.GetNamespace(),
		"name":      exemplar.GetName(),
	}
	if gvk, err := apiutil.GVKForObject(exemplar, sc.Scheme); err == nil {
		infoMap["kind"] = gvk.GroupKind()
	}
	return infoMap
}
