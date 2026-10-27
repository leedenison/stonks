// Package ptr holds helpers over pointers used as optional values.
package ptr

// Equal reports whether a and b are both nil or point at equal values.
func Equal[T comparable](a, b *T) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// To returns a pointer to v.
func To[T any](v T) *T { return &v }

// Deref returns *p, or def when p is nil.
func Deref[T any](p *T, def T) T {
	if p == nil {
		return def
	}
	return *p
}
