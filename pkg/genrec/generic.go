package genrec

import (
	"context"
	"fmt"
	"github.com/go-logr/logr"
	apiobjects "github.com/seatgeek/k8s-reconciler-generic/apiobjects"
	"github.com/seatgeek/k8s-reconciler-generic/apiobjects/apiutils"
	"github.com/seatgeek/k8s-reconciler-generic/pkg/k8sutil"
	v1 "k8s.io/api/core/v1"
	kmetav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"strings"
)

// Subject is an interface implemented by the class representing the CRD that is being reconciled.
type Subject interface {
	client.Object
	IsSuspended() bool
}

type Reconciler[S Subject, C any] struct {
	Logic    Logic[S, C]
	Client   k8sutil.SchemedClient
	Recorder record.EventRecorder

	KeyNamespace     string
	CurrentPartition string
}

// Logic is a bundle of methods that define the actual behavior of the generic reconciliation process
// when attached to some concrete CRD type.
type Logic[S Subject, C any] interface {
	// NewSubject creates a new, empty, instance of the subject CRD type.
	NewSubject() S
	// GetConfig returns a "config" for a new reconciliation Context. Config is used to pass contextual or
	// environmental data into the Context, so that it can be used in generators. Where the type S designates the CRD
	// itself, the type C is an ancillary data payload, you can put any struct there with any fields useful to
	// reconciliation.
	GetConfig() C
	// IsSubjectNil checks if a particular instance of the subject type is nil.
	IsSubjectNil(S) bool
	// IsStatusEqual compares the statuses of the given subjects and checks if they're the same.
	IsStatusEqual(S, S) bool
	// ConfigureController is called once at startup to associate informers and caches.
	ConfigureController(*builder.Builder, cluster.Cluster) error
	// FinalizerKey returns the finalizer to attach to subjects managed by this operator, return "" to disable
	// finalization.
	FinalizerKey() string
	// Finalize is called at least once when a subject is deleted, and must succeed before the finalizer key
	// can be removed from the subject. It must be idempotent.
	Finalize(*Context[S, C]) error
	// Validate checks the validity of the provided subject and prevents reconciliation if invalid.
	// TODO we should wire this up through a ValidatingWebhookConfiguration to prevent pain.
	Validate(S) error
	// FillDefaults fills in default values into the subject's Spec. In theory this could be hooked up
	// to a defaulting webhook BUT those are not very fun because it persists the defaults. So for now, this is
	// just-in-time to set defaults into the spec before generating resources.
	FillDefaults(*Context[S, C]) error
	// ObserveResources should collect all the resources downstream from the subject.
	ObserveResources(*Context[S, C]) (Resources, error)
	// GenerateResources should create the desired state of all resources downstream from the subject.
	GenerateResources(*Context[S, C]) (Resources, error)
	// FillStatus should fill in the status fields of the subject, including the provided SubjectStatus.
	FillStatus(*Context[S, C], Resources, apiobjects.SubjectStatus) error
	// ResourceIssues looks at the given object and determines any issues, i.e. underreplicated, unavailable, etc.
	ResourceIssues(obj client.Object) []string
	// ExtraLabelsForObject returns additional labels to add to generated objects
	ExtraLabelsForObject(c *Context[S, C], tier, suffix string) map[string]string
	// ExtraAnnotationsForObject returns additional annotations to add to generated objects
	ExtraAnnotationsForObject(c *Context[S, C], tier, suffix string) map[string]string
}

// WithoutFinalizationMixin ia a convenience type to embed in your Logic if you don't need finalization.
type WithoutFinalizationMixin[S Subject, C any] struct{}

func (t WithoutFinalizationMixin[_, _]) FinalizerKey() string {
	return "" // we do not set a finalizer
}

func (t WithoutFinalizationMixin[S, C]) Finalize(_ *Context[S, C]) error {
	return nil
}

type Context[S Subject, C any] struct {
	types.NamespacedName
	context.Context
	record.EventRecorder
	Subject S
	Owner   *Reconciler[S, C]
	Config  C
	Log     logr.Logger
	Client  *k8sutil.ContextClient
}

func (c *Context[_, C]) GetConfig() C {
	return c.Config
}

func (c *Context[_, _]) GetClient() *k8sutil.ContextClient {
	return c.Client
}

func (c *Context[_, _]) ObjectName(tier, suffix string) string {
	names := []string{c.Name}
	if tier != "" {
		names = append(names, tier)
	}
	if suffix != "" {
		names = append(names, suffix)
	}
	return strings.Join(names, "-")
}

// SelectorForObject returns a LabelSelector matching an object owned by the same CRD instance with the specified tier
// and suffix.
func (c *Context[_, _]) SelectorForObject(tier, suffix string) *kmetav1.LabelSelector {
	var componentParts []string
	if tier != "" {
		componentParts = append(componentParts, tier)
	}
	if suffix != "" {
		componentParts = append(componentParts, suffix)
	}
	return &kmetav1.LabelSelector{MatchLabels: map[string]string{
		c.Owner.LabelKey("owner"):     c.Name,
		c.Owner.LabelKey("component"): strings.Join(componentParts, "-"),
	}}
}

// SelectorForMeta returns the subset of labels in the provided ObjectMeta that we need for a selector.
// this is only guaranteed to work if the passed ObjectMeta was itself generated by the ObjectMeta method.
func (c *Context[_, _]) SelectorForMeta(om kmetav1.ObjectMeta) *kmetav1.LabelSelector {
	componentKey := c.Owner.LabelKey("component")
	return &kmetav1.LabelSelector{MatchLabels: map[string]string{
		c.Owner.LabelKey("owner"): c.Name,
		componentKey:              om.Labels[componentKey],
	}}
}

// ObjectMeta returns the ObjectMeta that a controller-generated object with the provided tier and suffix should use.
// The returned object would always be matchable by SelectorForObject, but also may include extra labels and annotations
// provided by the Logic instance.
func (c *Context[_, _]) ObjectMeta(tier, suffix string) kmetav1.ObjectMeta {
	// labels: owner (crd instance .name) + component (tier + suffix) uniquely identify a thing
	return kmetav1.ObjectMeta{
		Name:      c.ObjectName(tier, suffix),
		Namespace: c.Namespace,
		Labels: apiutils.CombineMaps(
			c.Owner.Logic.ExtraLabelsForObject(c, tier, suffix),
			c.SelectorForObject(tier, suffix).MatchLabels,
			map[string]string{
				c.Owner.LabelKey("tier"): tier,
			},
		),
		Annotations: c.Owner.Logic.ExtraAnnotationsForObject(c, tier, suffix),
	}
}

func (c *Context[_, _]) ResolveSecretKeyRef(ref apiobjects.SecretKeyRef) (string, error) {
	// The below call is memoized by the client-go informers and caches. It will not actually
	// hit the APIserver every time.
	s, err := k8sutil.Fetch[v1.Secret](ref.Secret, c.Client)
	if err != nil {
		return "", err // this is NOT a not found error!
	}

	if s != nil && s.Data != nil {
		if bytes, ok := s.Data[ref.Key]; ok {
			return string(bytes), nil
		}
	}

	if ref.Optional != nil && *ref.Optional {
		return "", nil
	}

	return "", fmt.Errorf("required secret %q or key %q not found", ref.Secret, ref.Key)
}

func (g *Reconciler[_, _]) SetupWithManager(mgr ctrl.Manager) error {
	cb := ctrl.NewControllerManagedBy(mgr)
	cb.For(g.Logic.NewSubject(), builder.WithPredicates(predicate.Or(
		predicate.GenerationChangedPredicate{},
		predicate.AnnotationChangedPredicate{},
	)))
	if err := g.Logic.ConfigureController(cb, mgr); err != nil {
		return err
	}
	return cb.Complete(g)
}

func (g *Reconciler[S, C]) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	rctx := &Context[S, C]{
		NamespacedName: request.NamespacedName,
		Context:        ctx,
		Owner:          g,
		Config:         g.Logic.GetConfig(),
		EventRecorder:  g.Recorder,
		Log:            log.FromContext(ctx),
		Client: &k8sutil.ContextClient{
			SchemedClient: g.Client,
			Context:       ctx,
			Namespace:     request.Namespace,
		},
	}
	if rctx.EventRecorder == nil {
		rctx.EventRecorder = &record.FakeRecorder{}
	}
	return g.reconcile(rctx)
}

func (g *Reconciler[S, C]) reconcile(ctx *Context[S, C]) (reconcile.Result, error) {
	var err error

	// step 1:
	// get the actual subject and make sure it is eligible for us to reconcile it.
	// exit immediately if it is ineligible, for a variety of reasons:

	if ctx.Subject, err = k8sutil.FetchInto(g.Logic.NewSubject, ctx.Name, ctx.Client); err != nil {
		return reconcile.Result{}, err
	}

	if g.Logic.IsSubjectNil(ctx.Subject) {
		ctx.Log.Info("subject is physically deleted")
		return reconcile.Result{}, err
	}

	if subjPartition := ctx.Subject.GetAnnotations()[g.LabelKey("partition")]; g.CurrentPartition != subjPartition {
		ctx.Log.Info("subject resides in a different controller partition",
			"subjPartition", subjPartition,
			"currentPartition", g.CurrentPartition)
		return reconcile.Result{}, nil
	}

	if ctx.Subject.IsSuspended() {
		ctx.Log.Info("subject is suspended")
		return reconcile.Result{}, nil
	}

	if err = g.Logic.Validate(ctx.Subject); err != nil {
		ctx.Log.Error(err, "subject failed validation, cannot reconcile")
		return reconcile.Result{}, err
	}

	if fk := g.Logic.FinalizerKey(); fk != "" {
		if result, err := g.manageFinalizers(ctx, fk); err != nil {
			return reconcile.Result{}, err
		} else if result != nil {
			// non-nil result here means *stop reconciling*
			return *result, nil
		}
	}

	ctx.Log.Info("reconciling live object")
	origSubject := ctx.Subject.DeepCopyObject().(S) // copy the object now to compare it later

	if err = g.Logic.FillDefaults(ctx); err != nil {
		// NOTE: we FillDefaults _after_ Validate, it is allowed to fill illegal defaults, e.g. for derived fields
		return reconcile.Result{}, err
	}

	// step 2:
	// observe actual resources and generate desired resources
	// these are used later to power updating the status and also changing the real state of these objects
	// to match the desired state

	var observedResources, desiredResources Resources

	if observedResources, err = g.Logic.ObserveResources(ctx); err != nil {
		return reconcile.Result{}, err
	}

	if desiredResources, err = g.Logic.GenerateResources(ctx); err != nil {
		return reconcile.Result{}, err
	}

	// step 3: update status
	// generically, we only update two things, but we delegate to Logic[S] in case there are more things to
	// set. the update is allowed to see the observed resources.

	resourceDiffs := DiffResources(observedResources, desiredResources)
	if err = g.Logic.FillStatus(ctx, observedResources, apiobjects.SubjectStatus{
		ObservedGeneration: ctx.Subject.GetGeneration(),
		Issues:             resourceDiffs.Issues(g.Logic.ResourceIssues),
	}); err != nil {
		return reconcile.Result{}, err
	}

	if !g.Logic.IsStatusEqual(origSubject, ctx.Subject) {
		if err = g.Client.Status().Update(ctx, ctx.Subject); err != nil {
			return reconcile.Result{}, err
		}
	}

	// step 4:
	// reconcile actual stuff. the part you've been waiting for. this is where this "generic" reconciler is really
	// helpful. the Logic[S] does not need to do anything for pure api reconciliation. this generic implementation
	// takes care of:
	//   1. comparing observed and desired, aligning them based on the .Key
	//   2. figuring out which resources are deleted / created / updated
	//   3. for those that are updated, uses annotation-based merge to minimize patch
	//   4. applies all the above to the apiserver.
	// you don't have to do anything other than return the correct observed / desired resources.

	for _, rd := range resourceDiffs {
		// apply the diffs in order (the same order as returned by GenerateResources, with deletes prepended)
		if err = rd.Apply(ctx, g.Client, ctx.Subject); err != nil {
			return reconcile.Result{}, err
		}
	}

	return reconcile.Result{}, nil
}

func (g *Reconciler[S, C]) manageFinalizers(ctx *Context[S, C], fk string) (*reconcile.Result, error) {
	hasOurFinalizer := controllerutil.ContainsFinalizer(ctx.Subject, fk)
	if ds := ctx.Subject.GetDeletionTimestamp(); !ds.IsZero() {
		ctx.Log.Info("subject is logically deleted", "deletionTimestamp", ds)
		if !hasOurFinalizer {
			return &reconcile.Result{}, nil // we already finalized it
		}
		ctx.Log.Info("subject needs finalization", "finalizer", fk)
		if err := g.Logic.Finalize(ctx); err != nil {
			return &reconcile.Result{}, err
		}
		ctx.Log.Info("subject was finalized", "finalizer", fk)
		controllerutil.RemoveFinalizer(ctx.Subject, fk)
		return &reconcile.Result{}, g.Client.Update(ctx, ctx.Subject)
	} else if !hasOurFinalizer {
		// Subject IS NOT logically deleted and needs finalizer added
		ctx.Log.Info("subject is live but missing finalizer key, adding", "finalizer", fk)
		controllerutil.AddFinalizer(ctx.Subject, fk)
		return &reconcile.Result{}, g.Client.Update(ctx, ctx.Subject)
	}
	return nil, nil
}

func (g *Reconciler[_, _]) LabelKey(s string) string {
	if g.KeyNamespace != "" {
		return fmt.Sprintf("%s/%s", g.KeyNamespace, s)
	}
	return s
}
