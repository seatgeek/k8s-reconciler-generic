package apiutils

func DerefOr[T any](v *T, d T) T {
	if v == nil {
		return d
	}
	return *v
}

func DefaultIfZero[T comparable](t, ifZero T) T {
	var zero T
	if t == zero {
		return ifZero
	}
	return t
}

func Ptr[T any](t T) *T {
	return &t
}

var (
	_true  = true
	_false = false
)

func Boolptr(v bool) *bool {
	if v {
		return &_true
	}
	return &_false
}

func CombineMaps[K comparable, V any](sms ...map[K]V) map[K]V {
	count := 0
	for _, sm := range sms {
		count += len(sm)
	}
	res := make(map[K]V, count)
	for _, sm := range sms {
		for k, v := range sm {
			res[k] = v
		}
	}
	return res
}
