package allowlist

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name string
		spec string
		want List
	}{
		{name: "empty", spec: "", want: nil},
		{name: "whitespace and empties", spec: " *@example.com , ,*@Example.org,", want: List{"*@example.com", "*@example.org"}},
		{name: "lowercased", spec: "Someone@EXAMPLE.com", want: List{"someone@example.com"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Parse(tc.spec)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("Parse(%q) mismatch (-want +got):\n%s", tc.spec, diff)
			}
		})
	}
}

func TestMatch(t *testing.T) {
	tests := []struct {
		name  string
		list  List
		email string
		want  bool
	}{
		{name: "empty list", list: nil, email: "someone@example.com", want: false},
		{name: "domain glob", list: List{"*@example.com"}, email: "someone@example.com", want: true},
		{name: "case folded", list: List{"*@example.com"}, email: "Someone@EXAMPLE.COM", want: true},
		{name: "other domain", list: List{"*@example.com"}, email: "someone@example.org", want: false},
		{name: "subdomain not covered", list: List{"*@example.com"}, email: "someone@mail.example.com", want: false},
		{name: "second pattern", list: List{"*@example.com", "one@example.org"}, email: "one@example.org", want: true},
		{name: "exact only", list: List{"one@example.org"}, email: "two@example.org", want: false},
		// / is legal in a local part and is not a separator here.
		{name: "slash in the local part", list: List{"*@example.com"}, email: "a/b@example.com", want: true},
		{name: "single character", list: List{"one?@example.com"}, email: "ones@example.com", want: true},
		{name: "single character spans one only", list: List{"one?@example.com"}, email: "onexy@example.com", want: false},
		{name: "several stars", list: List{"*+*@example.com"}, email: "one+tag@example.com", want: true},
		{name: "star matches nothing", list: List{"one*@example.com"}, email: "one@example.com", want: true},
		{name: "trailing star", list: List{"one@example.*"}, email: "one@example.com", want: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.list.Match(tc.email); got != tc.want {
				t.Errorf("%v.Match(%q) = %v, want %v", tc.list, tc.email, got, tc.want)
			}
		})
	}
}
