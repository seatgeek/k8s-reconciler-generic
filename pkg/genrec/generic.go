package genrec

import (
	"context"
	"errors"
	"fmt"
	"github.com/go-logr/logr"
	"github.com/seatgeek/k8s-reconciler-generic/apiobjects"
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
	"time"
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
	GetConfig(nn types.NamespacedName) C
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
	Finalize(*Context[S, C]) (FinalizationAction, error)
	// Validate checks the validity of the provided subject and prevents reconciliation if invalid.
	// It is called in both the validating webhook AND during reconciliation as a preflight check.
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
	// ApplyUnmanaged is called after all managed resources have been applied successfully, to give the Logic
	// a chance to reconcile non-k8s resources.
	ApplyUnmanaged(*Context[S, C]) error
	// ReconcileComplete runs unconditionally after a reconciliation, and sees the final context, any errors, and
	// the terminal status of the reconcile attempt. It is a good place to do logging/metrics.
	ReconcileComplete(*Context[S, C], ReconciliationStatus, error)
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
	Subject      S
	Owner        *Reconciler[S, C]
	Config       C
	Log          logr.Logger
	Started      time.Time
	Client       *k8sutil.ContextClient
	RequeueAfter *time.Duration
	Finalization FinalizationAction
}

func (c *Context[_, C]) GetConfig() C {
	return c.Config
}

func (c *Context[_, _]) GetSubjectNamespace() string {
	return c.Namespace
}

func (c *Context[_, _]) GetSubjectUID() types.UID {
	return c.Subject.GetUID()
}

func (c *Context[_, _]) GetClient() *k8sutil.ContextClient {
	return c.Client
}

func (c *Context[_, _]) IsFinalizing() bool {
	return c.Finalization != ""
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

// SetRequeue configures the controller to requeue the same object after a certain amount of time. During the lifetime
// of a context, user code (in Logic[] implementations) may call this multiple times, and the lowest value wins.
// Note that setting a requeue is NOT an error. And you can create a requeue for an otherwise-successful reconciliation,
// for example because you are synchronizing non-k8s resources. If you need to abort reconciliation, use an error.
func (c *Context[_, _]) SetRequeue(after time.Duration) {
	if c.RequeueAfter == nil || after < *(c.RequeueAfter) {
		// requeue after the minimum requested time
		c.RequeueAfter = &after
	}
}

func (g *Reconciler[S, C]) SetupWithManager(mgr ctrl.Manager) error {
	cb := ctrl.NewControllerManagedBy(mgr)
	subj := g.Logic.NewSubject()
	cb.For(subj, builder.WithPredicates(predicate.Or(
		predicate.GenerationChangedPredicate{},
		predicate.AnnotationChangedPredicate{},
	)))
	if err := g.Logic.ConfigureController(cb, mgr); err != nil {
		return err
	}
	if err := cb.Complete(g); err != nil {
		return err
	}
	wb := ctrl.NewWebhookManagedBy(mgr).For(subj).WithValidator(&validatingWebhookShim[S, C]{Logic: g.Logic})
	if mwl, ok := g.Logic.(MutatingWebhookLogic[S, C]); ok {
		// if your Logic implements MutatingWebhookLogic, we will hook it up:
		wb = wb.WithDefaulter(&mutatingWebhookShim[S, C]{Reconciler: g, MutatingWebhookLogic: mwl})
	}
	return wb.Complete()
}

// the below are not really "errors", they are special values
// to influence the abstraction control flow:

var ErrAbort = errors.New("abort reconciliation")
var ErrRetry = errors.New("abort reconciliation with requeue")

func (g *Reconciler[S, C]) newContext(ctx context.Context, nn types.NamespacedName) *Context[S, C] {
	rctx := &Context[S, C]{
		NamespacedName: nn,
		Context:        ctx,
		Owner:          g,
		Config:         g.Logic.GetConfig(nn),
		EventRecorder:  g.Recorder,
		Log:            log.FromContext(ctx),
		Started:        time.Now(),
		Client: &k8sutil.ContextClient{
			SchemedClient: g.Client,
			Context:       ctx,
			Namespace:     nn.Namespace,
		},
	}
	if rctx.EventRecorder == nil {
		rctx.EventRecorder = &record.FakeRecorder{}
	}
	return rctx
}

func (g *Reconciler[S, C]) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	rctx := g.newContext(ctx, request.NamespacedName)

	err, terminalState := g.reconcile(rctx)
	g.Logic.ReconcileComplete(rctx, terminalState, err)

	if err != nil {
		if errors.Is(err, ErrAbort) {
			return reconcile.Result{}, nil
		} else if errors.Is(err, ErrRetry) {
			return reconcile.Result{Requeue: true}, nil
		} else {
			// these are real errors that will trigger incrementing kubebuilder's
			// controller_runtime_reconcile_errors_total metric:
			return reconcile.Result{}, fmt.Errorf("reconciliation failed in state %v: %w", terminalState, err)
		}
	}

	if rctx.RequeueAfter != nil {
		return reconcile.Result{
			Requeue:      true,
			RequeueAfter: *rctx.RequeueAfter,
		}, nil
	}

	return reconcile.Result{}, nil
}

type ReconciliationStatus string

const (
	FetchError             ReconciliationStatus = "FetchError"
	SubjectNotFound        ReconciliationStatus = "SubjectNotFound"
	PartitionMismatch      ReconciliationStatus = "PartitionMismatch"
	SubjectSuspended       ReconciliationStatus = "SubjectSuspended"
	SubjectInvalid         ReconciliationStatus = "SubjectInvalid"
	AlreadyFinalized       ReconciliationStatus = "AlreadyFinalized"
	FinalizationError      ReconciliationStatus = "FinalizationError"
	FinalizersChanged      ReconciliationStatus = "FinalizersChanged"
	FillDefaultsError      ReconciliationStatus = "FillDefaultsError"
	ObserveResourcesError  ReconciliationStatus = "ObserveResourcesError"
	GenerateResourcesError ReconciliationStatus = "GenerateResourcesError"
	FillStatusError        ReconciliationStatus = "FillStatusError"
	UpdateStatusError      ReconciliationStatus = "UpdateStatusError"
	ApplyError             ReconciliationStatus = "ApplyError"
	Okay                   ReconciliationStatus = "Okay"
)

func (g *Reconciler[S, C]) reconcile(ctx *Context[S, C]) (error, ReconciliationStatus) {
	var err error

	// step 1:
	// get the actual subject and make sure it is eligible for us to reconcile it.
	// exit immediately if it is ineligible, for a variety of reasons:

	if ctx.Subject, err = k8sutil.FetchInto(g.Logic.NewSubject, ctx.Name, ctx.Client); err != nil {
		return err, FetchError
	}

	if g.Logic.IsSubjectNil(ctx.Subject) {
		ctx.Log.Info("subject is physically deleted")
		return nil, SubjectNotFound
	}

	if subjPartition := ctx.Subject.GetAnnotations()[g.LabelKey("partition")]; g.CurrentPartition != subjPartition {
		// do not allow subsequent code to see this subject anywhere, even in ReconcileComplete.
		// it is not ours to observe:
		var nilS S
		ctx.Subject = nilS
		return nil, PartitionMismatch
	}

	ctx.Log = ctx.Log.WithValues("subjectUID", ctx.Subject.GetUID())

	if ctx.Subject.IsSuspended() {
		ctx.Log.Info("subject is suspended")
		return nil, SubjectSuspended
	}

	if err = g.Logic.Validate(ctx.Subject); err != nil {
		ctx.Log.Error(err, "subject failed validation, cannot reconcile")
		return err, SubjectInvalid
	}

	if finalizerKey := g.Logic.FinalizerKey(); finalizerKey != "" {
		if action, err, rs := g.manageFinalizers(ctx, finalizerKey); err != nil {
			return err, rs
		} else if action == FinalizationCompleted {
			return nil, rs
		} else if action != "" {
			ctx.Finalization = action
		}
	}

	origSubject := ctx.Subject.DeepCopyObject().(S) // copy the object now to compare it later

	if err = g.Logic.FillDefaults(ctx); err != nil {
		// NOTE: we FillDefaults _after_ Validate, it is allowed to fill illegal defaults, e.g. for derived fields
		return err, FillDefaultsError
	}

	// step 2:
	// observe actual resources and generate desired resources
	// these are used later to power updating the status and also changing the real state of these objects
	// to match the desired state

	var observedResources, desiredResources Resources

	if observedResources, err = g.Logic.ObserveResources(ctx); err != nil {
		return err, ObserveResourcesError
	}

	if desiredResources, err = g.Logic.GenerateResources(ctx); err != nil {
		return err, GenerateResourcesError
	}

	// step 3: update status
	// generically, we only update two things, but we delegate to Logic[S] in case there are more things to
	// set. the update is allowed to see the observed resources.

	resourceDiffs := DiffResources(observedResources, desiredResources)
	if err = g.Logic.FillStatus(ctx, observedResources, apiobjects.SubjectStatus{
		ObservedGeneration: ctx.Subject.GetGeneration(),
		Issues:             resourceDiffs.Issues(g.Logic.ResourceIssues),
	}); err != nil {
		return err, FillStatusError
	}

	if !g.Logic.IsStatusEqual(origSubject, ctx.Subject) {
		// If we do not deep copy the object before updating Status, this call overwrites the Spec in the
		// referent, and we lose anything that was added in FillDefaults. Logic in ApplyUnmanaged, if any,
		// should see the spec with defaults filled in!
		if err = g.Client.Status().Update(ctx, ctx.Subject.DeepCopyObject().(S)); err != nil {
			return err, UpdateStatusError
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
			return err, ApplyError
		}
	}

	if err = g.Logic.ApplyUnmanaged(ctx); err != nil {
		return err, ApplyError
	}

	if ctx.Finalization == FinalizeAfterReconciliation {
		ctx.Log.Info("subject is being finalized after successful reconcile")
		ctx.SetRequeue(0)
		controllerutil.RemoveFinalizer(ctx.Subject, g.Logic.FinalizerKey())
		return g.Client.Update(ctx, ctx.Subject), FinalizersChanged
	}

	return nil, Okay
}

type FinalizationAction string

const (
	// FinalizationImpossible means that the object or system state prevents object finalization right now. You
	// should always record an event in this state, to give a hint to callers what they need to do to unblock
	// deletion. When finalization is ignored, it means the object is trying to be deleted, but we'll continue
	// reconciling as usual. Every subsequent reconciliation, the Finalize() method will be called again,
	// and can continue returning FinalizationImpossible for as long as needed.
	FinalizationImpossible FinalizationAction = "Impossible"
	// FinalizationCompleted should be returned when the object can be deleted immediately. Reconciliation
	// will be aborted, the finalizer removed, no generators will run, and k8s will start to collect the object and its
	// descendents.
	FinalizationCompleted FinalizationAction = "Completed"
	// FinalizeAfterReconciliation indicates that we should remove the finalizer _after_ reconciling all child objects
	// successfully one last time. If any generator is unsuccessful, this will behave like FinalizationImpossible until
	// the next loop, where it can try again.
	FinalizeAfterReconciliation FinalizationAction = "AfterReconciliation"
)

func (g *Reconciler[S, C]) manageFinalizers(ctx *Context[S, C], fk string) (FinalizationAction, error, ReconciliationStatus) {
	hasOurFinalizer := controllerutil.ContainsFinalizer(ctx.Subject, fk)
	if ds := ctx.Subject.GetDeletionTimestamp(); !ds.IsZero() {
		ctx.Log.Info("subject is logically deleted", "deletionTimestamp", ds)
		if !hasOurFinalizer {
			return FinalizationCompleted, nil, AlreadyFinalized // we already finalized it
		}
		mode, err := g.Logic.Finalize(ctx)
		if err != nil {
			return "", err, FinalizationError
		}

		ctx.Log.Info("handling finalization this reconcile", "action", mode)
		if mode == FinalizationCompleted {
			ctx.SetRequeue(0)
			ctx.Log.Info("subject was finalized", "finalizer", fk)
			controllerutil.RemoveFinalizer(ctx.Subject, fk)
			return FinalizationCompleted, g.Client.Update(ctx, ctx.Subject), FinalizersChanged
		}
		
		return mode, nil, Okay
	} else if !hasOurFinalizer {
		// Subject IS NOT logically deleted and needs finalizer added
		ctx.Log.Info("subject is live but missing finalizer key, adding", "finalizer", fk)
		controllerutil.AddFinalizer(ctx.Subject, fk)
		ctx.SetRequeue(0)
		// we return FinalizationCompleted to abort reconciliation and retry with the finalizer added
		return FinalizationCompleted, g.Client.Update(ctx, ctx.Subject), FinalizersChanged
	}
	return "", nil, Okay
}

func (g *Reconciler[_, _]) LabelKey(s string) string {
	if g.KeyNamespace != "" {
		return fmt.Sprintf("%s/%s", g.KeyNamespace, s)
	}
	return s
}
