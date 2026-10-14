# A not found answer is evidence of retirement

The identifier event endpoints of both providers report renames only.

Retirements are recorded from two kinds of evidence:

- Massive's ticker listing with `active=false` states `delisted_utc`. This is a witnessed
  retirement on that date.
- A provider answers that it does not know a value it has asserted before. The answer is
  a negative assertion: at the moment of the fetch, the value names nothing at that
  provider. Any endpoint can make one. An identity lookup makes one in this milestone,
  and a price fetch will make one later.

A negative assertion and the provider's last positive assertion of the value bound an
inferred retirement. The retirement takes effect somewhere between the two, in the same
way that two assertions naming different instruments bound an inferred event.

A negative assertion counts only under two conditions. The same provider must have
asserted the value before. A provider that never served a value cannot show that it
retired. The answer must state that the value is unknown. A refusal for plan,
history window or quota says nothing about the value.
