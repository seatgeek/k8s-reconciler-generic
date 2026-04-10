package genrec

import (
	"context"
	"encoding/base64"
	"testing"

	om "github.com/banzaicloud/k8s-objectmatcher/patch"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/seatgeek/k8s-reconciler-generic/pkg/k8sutil"
)

// configMapWithCorruptLastAppliedAnnotation returns a ConfigMap whose
// banzaicloud.com/last-applied annotation decodes to bytes that sniff as
// application/zip but are not a valid zip, so patch.Calculate fails while
// reading original configuration.
func configMapWithCorruptLastAppliedAnnotation() *corev1.ConfigMap {
	corruptZip := []byte{'P', 'K', 0x03, 0x04, 0x00, 0x00, 0x00}
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "test-cm",
			Annotations: map[string]string{
				om.LastAppliedConfig: base64.StdEncoding.EncodeToString(corruptZip),
			},
		},
		Data: map[string]string{"key": "observed"},
	}
}

func TestResourceDiff_Apply_DeleteOnPatchCalculationError(t *testing.T) {
	normalObserved := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "test-cm",
		},
		Data: map[string]string{"key": "observed"},
	}
	desiredDiffers := normalObserved.DeepCopy()
	desiredDiffers.Data["key"] = "desired"

	tests := []struct {
		name                          string
		observed                      *corev1.ConfigMap
		desired                       *corev1.ConfigMap
		deleteOnPatchCalculationError bool
		wantDelete                    bool
		wantErr                       bool
		wantKey                       string // Data["key"] when object still exists
	}{
		{
			name:                          "deletes when patch calculation fails and opt is set",
			observed:                      configMapWithCorruptLastAppliedAnnotation(),
			desired:                       desiredDiffers.DeepCopy(),
			deleteOnPatchCalculationError: true,
			wantDelete:                    true,
			wantErr:                       false,
			wantKey:                       "",
		},
		{
			name:                          "returns error when patch calculation fails and opt is unset",
			observed:                      configMapWithCorruptLastAppliedAnnotation(),
			desired:                       desiredDiffers.DeepCopy(),
			deleteOnPatchCalculationError: false,
			wantDelete:                    false,
			wantErr:                       true,
			wantKey:                       "observed",
		},
		{
			name:                          "no-op when observed matches desired",
			observed:                      normalObserved.DeepCopy(),
			desired:                       normalObserved.DeepCopy(),
			deleteOnPatchCalculationError: true,
			wantDelete:                    false,
			wantErr:                       false,
			wantKey:                       "observed",
		},
		{
			name:                          "applies update when patch calculation succeeds",
			observed:                      normalObserved.DeepCopy(),
			desired:                       desiredDiffers.DeepCopy(),
			deleteOnPatchCalculationError: true,
			wantDelete:                    false,
			wantErr:                       false,
			wantKey:                       "desired",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			if err := corev1.AddToScheme(scheme); err != nil {
				t.Fatalf("AddToScheme: %v", err)
			}
			cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tt.observed.DeepCopy()).Build()
			sc := k8sutil.SchemedClient{Client: cl, Scheme: scheme}

			rd := ResourceDiff{
				Key:          "cm",
				Observed:     tt.observed.DeepCopy(),
				Desired:      tt.desired.DeepCopy(),
				ResourceOpts: ResourceOpts{DeleteOnPatchCalculationError: tt.deleteOnPatchCalculationError},
			}

			err := rd.Apply(context.Background(), sc, nil)
			if tt.wantErr && err == nil {
				t.Fatal("expected Apply error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("Apply: %v", err)
			}

			var got corev1.ConfigMap
			getErr := cl.Get(context.Background(), client.ObjectKeyFromObject(tt.observed), &got)
			stillExists := getErr == nil

			if tt.wantDelete {
				if stillExists {
					t.Fatal("expected object to be deleted after patch calculation error")
				}
				return
			}
			if !stillExists {
				t.Fatal("expected object to remain")
			}
			if got.Data["key"] != tt.wantKey {
				t.Fatalf("expected ConfigMap data key %q, got %q", tt.wantKey, got.Data["key"])
			}
		})
	}
}
