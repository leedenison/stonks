package db

import (
	"strings"
	"unicode"

	"google.golang.org/protobuf/reflect/protoreflect"
)

// Enum is a generated database enum.
type Enum interface {
	~string
	Valid() bool
}

// ProtoEnum is a generated protobuf enum.
type ProtoEnum interface {
	~int32
	Descriptor() protoreflect.EnumDescriptor
	Number() protoreflect.EnumNumber
}

// A database enum and its protobuf counterpart name their values alike: the
// value's word in the database, and in the proto the same word upper-cased
// after the enum's own name, as run_trigger's 'run' and RunTrigger's
// RUN_TRIGGER_RUN. The two conversions below rest on that, so a value added
// to one vocabulary crosses as soon as it is added to the other.

// FromProto converts a proto enum value to the database enum. UNSPECIFIED
// converts to "", and a number the proto does not define fails.
func FromProto[T Enum, P ProtoEnum](v P) (T, bool) {
	desc := v.Descriptor()
	value := desc.Values().ByNumber(v.Number())
	if value == nil {
		return "", false
	}
	word := strings.TrimPrefix(string(value.Name()), prefix(desc))
	if word == "UNSPECIFIED" {
		return "", true
	}
	t := T(strings.ToLower(word))
	return t, t.Valid()
}

// ToProto converts a database enum value to the proto enum. A value the
// proto does not name, "" among them, converts to UNSPECIFIED.
func ToProto[P ProtoEnum, T ~string](v T) P {
	var zero P
	desc := zero.Descriptor()
	value := desc.Values().ByName(protoreflect.Name(prefix(desc) + strings.ToUpper(string(v))))
	if value == nil {
		return zero
	}
	return P(value.Number())
}

// prefix returns the enum's name in upper snake case with a trailing
// underscore, as RunTrigger to RUN_TRIGGER_.
func prefix(desc protoreflect.EnumDescriptor) string {
	var b strings.Builder
	for i, r := range string(desc.Name()) {
		if i > 0 && unicode.IsUpper(r) {
			b.WriteByte('_')
		}
		b.WriteRune(unicode.ToUpper(r))
	}
	b.WriteByte('_')
	return b.String()
}
