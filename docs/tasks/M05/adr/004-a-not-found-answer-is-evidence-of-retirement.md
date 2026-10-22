# A not found answer is evidence of retirement

Massive's identifier event endpoint reports renames only.

Retirements are recorded from two kinds of evidence:

- Massive's ticker listing with `active=false` states `delisted_utc`. This is a witnessed
  retirement on that date.
- A provider answers that it does not know a value it has asserted before. The answer is
  a negative assertion: on the day of the answer, the value names nothing at that
  provider. A point in time lookup makes its negative assertion on the day its
  request names. Any endpoint can make one. An identity lookup makes one in this milestone,
  and a price fetch will make one later.

A negative assertion and the provider's last positive assertion of the value bound an
inferred retirement. The retirement takes effect somewhere between the two, in the same
way that two assertions naming different instruments bound an inferred event.

A negative assertion counts only under two conditions. The same provider must have
asserted the value before. A provider that never served a value cannot show that it
retired. The answer must state that the value is unknown. A refusal for plan,
history window or quota says nothing about the value.
