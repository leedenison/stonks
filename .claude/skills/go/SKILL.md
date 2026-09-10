---
name: go
description: Go style and conventions for the Stonks backend (server/) -- formatting, lint handling, doc comments, and Go-specific test idioms including mocks and goroutine leak checks. Use when writing or changing any Go code, including adding a configuration value or choosing an error code at a service boundary.
---

# Go

## Formatting

`gofmt`, enforced by `make fmt-check`.

## Linting Handling

Do not silence a finding with `//nolint` without saying why on the same line;
prefer fixing it, or adding a scoped rule to `.golangci.yml`.

Never discard an error to satisfy a linter. 

## Doc Comments

Every exported identifier and package carries a doc comment beginning with its name.

Go package docs carry the specification of the system.  You must read the package
docs for any package being modified, and each of its parent packages, when planning
a change.  You must also review any changes made against package docs for the
package, and each of its parents, for contradictions and inconsistencies before
committing changes.  Contradictions and inconsistencies must be reconciled by
updating the docs, if the conflicting code change was intentional, or by updating
the code if the conflicting code change did not intend to change the spec.  Always
highlight such reconciliation to the user.

## Tests

Tests are colocated as `*_test.go` (do not place tests in the same files as the code they
test). Control flow is standard library `testing`: `require.NoError` aborts the test from
inside a helper.

Use **go-cmp** for structs and slices, and `!=` for scalars.

Table-driven tests are the default shape:

```go
tests := []struct {
    name    string
    input   string
    want    string
    wantErr bool
}{
    {name: "empty", input: "", wantErr: true},
}
for _, tc := range tests {
    t.Run(tc.name, func(t *testing.T) {
        got, err := Parse(tc.input)
        if (err != nil) != tc.wantErr {
            t.Fatalf("Parse(%q) error = %v, wantErr %v", tc.input, err, tc.wantErr)
        }
        if got != tc.want {
            t.Errorf("Parse(%q) = %q, want %q", tc.input, got, tc.want)
        }
    })
}
```

Name the case and put the inputs in the failure message. A failure should say what was
wrong without the reader opening the test.

`t.Fatalf` when continuing would panic or produce a second misleading failure; `t.Errorf`
otherwise, so one run reports everything that is broken.

### Mocks

Use gomock. Mock the narrow interface the package under test declares. The directive sits
in the file that declares the interface, and the mock is generated beside it:

```go
//go:generate go tool mockgen -source=server.go -destination=mock/store_mock.go -package=mock
```

There is no central mock package; a mock belongs to the package whose interface it implements.

```go
ctrl := gomock.NewController(t)
t.Cleanup(ctrl.Finish)
s := mock.NewMockPortfolioStore(ctrl)
s.EXPECT().GetPortfolio(gomock.Any(), gomock.Any()).Return(row, nil)
```

Set expectations only for what the test is about.

### Goroutines

A package that owns background goroutines verifies they are gone, with
[goleak](https://github.com/uber-go/goleak):

```go
func TestMain(m *testing.M) {
    goleak.VerifyTestMain(m)
}
```

What it catches is the component whose `Close` does not stop what it started. Only in the
packages that own goroutines.

## See also

The `protobuf` skill for the API definitions these handlers implement, and the
`unit-testing` and `integration-testing` skills.
