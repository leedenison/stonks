# A Datasource is a Row, Not an Environment Variable

The framework needs to know which datasources this instance may call, in what order their
answers are preferred, and what to authenticate with. Any running instance may hold a
different combination, so the answer cannot be compiled in.

The registry is the `datasources` table. The migration creates it empty and each instance
seeds its own rows, carrying the enabled state, the precedence, the credential and the
endpoint. Nothing about a datasource is read from the environment.

One binary therefore serves instances running different combinations, and the two things
that must travel together -- which provider to call and what to call it with -- are one
row rather than a name in one place and a secret in another. The endpoint is what lets a
test stack point the same integration at a stub.

Precedence is not unique. What a resolution needs is a total order, and `(precedence,
name)` gives one, so a datasource is placed between two others without renumbering them.

## Considered options

An environment variable per datasource. It splits the credential from the precedence, and
it cannot be read back by an administrator surface that lists what this instance runs.

## Consequences

An enabled row naming an integration the binary does not carry stops the service. The
alternative, warning and continuing, would silently omit a datasource the operator
believes is on, and a datasource consulted after an instrument already exists cannot
become its winner, so the omission would be permanent. Recovery is one `UPDATE`.

The registry is read at boot and a change takes a restart, so two concurrent runs cannot
see different precedence.

A read of the table serving an administrator must project the credential away.
