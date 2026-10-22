package genrec

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/seatgeek/k8s-reconciler-generic/apiobjects"
	"github.com/seatgeek/k8s-reconciler-generic/pkg/k8sutil"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const reconcileTestFinalizer = "test.io/cleanup"

type reconcileTestSubject struct {
	metav1.TypeMeta
	metav1.ObjectMeta
}

func (s *reconcileTestSubject) DeepCopyObject() runtime.Object {
	return &reconcileTestSubject{TypeMeta: s.TypeMeta, ObjectMeta: *s.ObjectMeta.DeepCopy()}
}

func (s *reconcileTestSubject) IsSuspended() bool {
	return false
}

type reconcileTestContext = Context[*reconcileTestSubject, any]

// reconcileTestLogic generates a single ConfigMap child named after the subject.
type reconcileTestLogic struct {
	finalizerKey         string
	finalizeAction       FinalizationAction
	status               ReconciliationStatus
	applyUnmanagedCalled bool
	// observeErr, when set, is returned by ObserveResources.
	observeErr error
	// completeErr is the error ReconcileComplete received.
	completeErr error
}

func (l *reconcileTestLogic) NewSubject() *reconcileTestSubject             { return &reconcileTestSubject{} }
func (l *reconcileTestLogic) GetConfig(_ types.NamespacedName) any          { return nil }
func (l *reconcileTestLogic) IsSubjectNil(s *reconcileTestSubject) bool     { return s == nil }
func (l *reconcileTestLogic) IsStatusEqual(_, _ *reconcileTestSubject) bool { return true }
func (l *reconcileTestLogic) ConfigureController(_ *builder.Builder, _ cluster.Cluster) error {
	return nil
}
func (l *reconcileTestLogic) FinalizerKey() string { return l.finalizerKey }
func (l *reconcileTestLogic) Finalize(_ *reconcileTestContext) (FinalizationAction, error) {
	return l.finalizeAction, nil
}
func (l *reconcileTestLogic) Validate(_ *reconcileTestSubject) error     { return nil }
func (l *reconcileTestLogic) FillDefaults(_ *reconcileTestContext) error { return nil }
func (l *reconcileTestLogic) ResourceIssues(_ client.Object) []string    { return nil }
func (l *reconcileTestLogic) ApplyUnmanaged(_ *reconcileTestContext) error {
	l.applyUnmanagedCalled = true
	return nil
}
func (l *reconcileTestLogic) FillStatus(_ *reconcileTestContext, _ Resources, _ apiobjects.SubjectStatus) error {
	return nil
}
func (l *reconcileTestLogic) ExtraLabelsForObject(_ *reconcileTestContext, _, _ string) map[string]string {
	return nil
}
func (l *reconcileTestLogic) ExtraAnnotationsForObject(_ *reconcileTestContext, _, _ string) map[string]string {
	return nil
}
func (l *reconcileTestLogic) ReconcileComplete(_ *reconcileTestContext, rs ReconciliationStatus, err error) {
	l.status = rs
	l.completeErr = err
}

func (l *reconcileTestLogic) ObserveResources(c *reconcileTestContext) (Resources, error) {
	if l.observeErr != nil {
		return nil, l.observeErr
	}
	cm := &corev1.ConfigMap{}
	if err := c.Client.Get(c.Name, cm); err != nil {
		if apierrors.IsNotFound(err) {
			return Resources{}, nil
		}
		return nil, err
	}
	return Resources{{Key: "child", Object: cm}}, nil
}

func (l *reconcileTestLogic) GenerateResources(c *reconcileTestContext) (Resources, error) {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Namespace: c.Namespace, Name: c.Name},
		Data:       map[string]string{"key": "value"},
	}
	return Resources{{Key: "child", Object: cm}}, nil
}

func TestReconcile_TerminatingSubject(t *testing.T) {
	deleted := metav1.NewTime(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))

	tests := []struct {
		name                     string
		finalizerKey             string
		finalizeAction           FinalizationAction
		deletionTimestamp        *metav1.Time
		finalizers               []string
		wantStatus               ReconciliationStatus
		wantChild                bool
		wantApplyUnmanagedCalled bool
	}{
		{
			name:                     "live subject without finalizer key is reconciled",
			wantStatus:               Okay,
			wantChild:                true,
			wantApplyUnmanagedCalled: true,
		},
		{
			name:              "terminating subject without finalizer key is skipped",
			deletionTimestamp: &deleted,
			finalizers:        []string{metav1.FinalizerDeleteDependents},
			wantStatus:        SubjectDeleting,
			wantChild:         false,
		},
		{
			name:                     "terminating subject with finalizer key keeps finalization path",
			finalizerKey:             reconcileTestFinalizer,
			finalizeAction:           FinalizeAfterReconciliation,
			deletionTimestamp:        &deleted,
			finalizers:               []string{reconcileTestFinalizer},
			wantStatus:               FinalizersChanged,
			wantChild:                true,
			wantApplyUnmanagedCalled: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			if err := corev1.AddToScheme(scheme); err != nil {
				t.Fatalf("AddToScheme: %v", err)
			}
			scheme.AddKnownTypeWithName(schema.GroupVersionKind{Group: "test.io", Version: "v1", Kind: "Subject"}, &reconcileTestSubject{})

			subject := &reconcileTestSubject{ObjectMeta: metav1.ObjectMeta{
				Namespace:         "default",
				Name:              "subject",
				UID:               "subject-uid",
				DeletionTimestamp: tt.deletionTimestamp,
				Finalizers:        tt.finalizers,
			}}
			cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(subject).Build()

			logic := &reconcileTestLogic{finalizerKey: tt.finalizerKey, finalizeAction: tt.finalizeAction}
			g := &Reconciler[*reconcileTestSubject, any]{
				Logic:  logic,
				Client: k8sutil.SchemedClient{Client: cl, Scheme: scheme},
			}

			if _, err := g.Reconcile(context.Background(), reconcile.Request{NamespacedName: client.ObjectKeyFromObject(subject)}); err != nil {
				t.Fatalf("Reconcile: %v", err)
			}

			if logic.status != tt.wantStatus {
				t.Errorf("status = %q, want %q", logic.status, tt.wantStatus)
			}

			err := cl.Get(context.Background(), client.ObjectKeyFromObject(subject), &corev1.ConfigMap{})
			if gotChild := err == nil; gotChild != tt.wantChild {
				t.Errorf("child exists = %v, want %v (get error: %v)", gotChild, tt.wantChild, err)
			}

			if logic.applyUnmanagedCalled != tt.wantApplyUnmanagedCalled {
				t.Errorf("ApplyUnmanaged called = %v, want %v", logic.applyUnmanagedCalled, tt.wantApplyUnmanagedCalled)
			}
		})
	}
}

func TestReconcile_ContextCancellation(t *testing.T) {
	canceled := func() context.Context {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		return ctx
	}
	expired := func() context.Context {
		ctx, cancel := context.WithDeadline(context.Background(), time.Unix(0, 0))
		t.Cleanup(cancel)
		return ctx
	}

	tests := []struct {
		name       string
		ctx        func() context.Context
		observeErr error
		wantErr    bool
	}{
		{
			name:       "shutdown cancels the reconcile context",
			ctx:        canceled,
			observeErr: fmt.Errorf("observe CiliumNetworkPolicies: %w", context.Canceled),
			wantErr:    false,
		},
		{
			name:       "reconcile context deadline is still an error",
			ctx:        expired,
			observeErr: fmt.Errorf("observe: %w", context.Canceled),
			wantErr:    true,
		},
		{
			name:       "cancel of a derived context is still an error",
			ctx:        context.Background,
			observeErr: fmt.Errorf("observe: %w", context.Canceled),
			wantErr:    true,
		},
		{
			name:       "unrelated error during shutdown is still an error",
			ctx:        canceled,
			observeErr: errors.New("boom"),
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			if err := corev1.AddToScheme(scheme); err != nil {
				t.Fatalf("AddToScheme: %v", err)
			}
			scheme.AddKnownTypeWithName(schema.GroupVersionKind{Group: "test.io", Version: "v1", Kind: "Subject"}, &reconcileTestSubject{})

			subject := &reconcileTestSubject{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "subject", UID: "subject-uid"}}
			cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(subject).Build()

			logic := &reconcileTestLogic{observeErr: tt.observeErr}
			g := &Reconciler[*reconcileTestSubject, any]{
				Logic:  logic,
				Client: k8sutil.SchemedClient{Client: cl, Scheme: scheme},
			}

			res, err := g.Reconcile(tt.ctx(), reconcile.Request{NamespacedName: client.ObjectKeyFromObject(subject)})
			if gotErr := err != nil; gotErr != tt.wantErr {
				t.Fatalf("Reconcile error = %v, wantErr %v", err, tt.wantErr)
			}
			if !res.IsZero() {
				t.Errorf("Reconcile result = %+v, want zero", res)
			}

			// the reconciliation must actually have reached ObserveResources, and ReconcileComplete
			// always sees the real error, whether or not it is reported to controller-runtime
			if logic.status != ObserveResourcesError {
				t.Errorf("status = %q, want %q", logic.status, ObserveResourcesError)
			}
			if !errors.Is(logic.completeErr, tt.observeErr) {
				t.Errorf("ReconcileComplete error = %v, want %v", logic.completeErr, tt.observeErr)
			}
		})
	}
}
