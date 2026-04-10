package genrec

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/seatgeek/k8s-reconciler-generic/pkg/k8sutil"
)

func TestResourceDiff_Apply_DeleteOnChange(t *testing.T) {
	observed := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "test-cm",
		},
		Data: map[string]string{"key": "observed"},
	}
	desired := observed.DeepCopy()
	desired.Data["key"] = "desired"

	tests := []struct {
		name           string
		deleteOnChange bool
		desired        *corev1.ConfigMap
		wantDelete     bool
	}{
		{
			name:           "DeleteOnChange deletes only when patch is non-empty",
			deleteOnChange: true,
			desired:        desired.DeepCopy(),
			wantDelete:     true,
		},
		{
			name:           "DeleteOnChange no-op when observed matches desired",
			deleteOnChange: true,
			desired:        observed.DeepCopy(),
			wantDelete:     false,
		},
		{
			name:           "without DeleteOnChange patch applies for updates",
			deleteOnChange: false,
			desired:        desired.DeepCopy(),
			wantDelete:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			if err := corev1.AddToScheme(scheme); err != nil {
				t.Fatalf("AddToScheme: %v", err)
			}
			cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(observed.DeepCopy()).Build()
			sc := k8sutil.SchemedClient{Client: cl, Scheme: scheme}

			rd := ResourceDiff{
				Key:          "cm",
				Observed:     observed.DeepCopy(),
				Desired:      tt.desired.DeepCopy(),
				ResourceOpts: ResourceOpts{DeleteOnChange: tt.deleteOnChange},
			}

			if err := rd.Apply(context.Background(), sc, nil); err != nil {
				t.Fatalf("Apply: %v", err)
			}

			var got corev1.ConfigMap
			err := cl.Get(context.Background(), client.ObjectKeyFromObject(observed), &got)
			stillExists := err == nil
			if tt.wantDelete && stillExists {
				t.Fatal("expected object to be deleted from the API when DeleteOnChange is set")
			}
			if !tt.wantDelete {
				if !stillExists {
					t.Fatal("expected object to remain when DeleteOnChange is false")
				}
				if got.Data["key"] != tt.desired.Data["key"] {
					t.Fatalf("expected ConfigMap data to match desired, got %q want %q", got.Data["key"], tt.desired.Data["key"])
				}
			}
		})
	}
}
