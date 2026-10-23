# Precedence decides between providers that disagree, and a finding records it

Two providers may assert different instruments for one value on the same day. The
system could leave a gap in validity between them, or let one win.

The assertion of the higher precedence datasource wins, as it does for identity answers.
The disagreement is recorded as a finding, so an administrator sees it. The run whose
write completes the pair records it, whichever of the two assertions it wrote. The
finding names the fetch key of the losing assertion. An attested assertion outranks
every datasource by design, so a pair it wins is not recorded.

A gap would leave every key dated in it unresolved, though one provider is usually right.
Precedence already expresses which provider the administrator trusts more.
