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
		wantDelete     bool
	}{
		{
			name:           "DeleteOnChange short-circuits before patch calculation",
			deleteOnChange: true,
			wantDelete:     true,
		},
		{
			name:           "without DeleteOnChange patch calculation runs for updates",
			deleteOnChange: false,
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
				Desired:      desired.DeepCopy(),
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
				if got.Data["key"] != "desired" {
					t.Fatalf("expected ConfigMap to be updated to desired data, got %q", got.Data["key"])
				}
			}
		})
	}
}
