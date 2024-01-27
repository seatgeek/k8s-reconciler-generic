package genrec

import (
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"reflect"
	"testing"
)

type testSubject struct {
	v1.TypeMeta
	v1.ObjectMeta
}

func (t *testSubject) DeepCopyObject() runtime.Object {
	return &testSubject{TypeMeta: t.TypeMeta, ObjectMeta: t.ObjectMeta}
}

func (t *testSubject) IsSuspended() bool {
	return false
}

func TestContext_SelectorForObject(t *testing.T) {
	type args struct {
		tier   string
		suffix string
	}
	type testCase struct {
		name    string
		context Context[*testSubject, any]
		args    args
		want    *v1.LabelSelector
	}
	testContext := Context[*testSubject, any]{
		NamespacedName: types.NamespacedName{Name: "testobj"},
		Owner:          &Reconciler[*testSubject, any]{KeyNamespace: "test.io"},
	}
	tests := []testCase{
		{
			name:    "empty",
			context: testContext,
			args:    args{},
			want: &v1.LabelSelector{
				MatchLabels: map[string]string{
					"test.io/owner":     "testobj",
					"test.io/component": "",
				},
			},
		},
		{
			name:    "tier-only",
			context: testContext,
			args:    args{tier: "web"},
			want: &v1.LabelSelector{
				MatchLabels: map[string]string{
					"test.io/owner":     "testobj",
					"test.io/component": "web",
				},
			},
		},
		{
			name:    "tier-and-suffix",
			context: testContext,
			args:    args{tier: "web", suffix: "public"},
			want: &v1.LabelSelector{
				MatchLabels: map[string]string{
					"test.io/owner":     "testobj",
					"test.io/component": "web-public",
				},
			},
		},
		{
			name:    "suffix-only",
			context: testContext,
			args:    args{suffix: "tls"},
			want: &v1.LabelSelector{
				MatchLabels: map[string]string{
					"test.io/owner":     "testobj",
					"test.io/component": "tls",
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.context.SelectorForObject(tt.args.tier, tt.args.suffix); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("SelectorForObject() = %v, want %v", got, tt.want)
			}
		})
	}
}
