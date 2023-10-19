package apiutils

import (
	"testing"
)

func TestHashSecretData(t *testing.T) {
	tests := []struct {
		name string
		arg  map[string][]byte
		want string
	}{
		{
			name: "simple1",
			arg: map[string][]byte{
				"a": []byte("foo"),
				"b": []byte("bar"),
				"c": []byte("bar"),
			},
			want: "ZFZ36JXVVK26KTUKGX7V6QV5TJODJSCG32SQAJS2Y2MYVJKSXPVQ",
		},
		{
			name: "simple2",
			arg: map[string][]byte{
				"x": []byte("foo"),
				"y": []byte("bar"),
				"z": []byte("baz"),
			},
			want: "5UPDGSA3BY2YBTEC3LVFX5JDUHDIDGMUQMO43DQILF5ECAELADEQ",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HashSecretData(tt.arg); got != tt.want {
				t.Errorf("HashSecret() = %v, want %v", got, tt.want)
			}
		})
	}
}
