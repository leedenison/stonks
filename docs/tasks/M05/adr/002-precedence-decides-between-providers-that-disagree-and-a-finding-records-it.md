# Precedence decides between providers that disagree, and a finding records it

Two providers may assert different instruments for one value on the same day. The
system could leave a gap in validity between them, or let one win.

The assertion of the higher precedence datasource wins, as it does for identity answers.
The disagreement is recorded as a finding of the run that met it, so an administrator
sees it.

A gap would leave every key dated in it unresolved, though one provider is usually right.
Precedence already expresses which provider the administrator trusts more.
