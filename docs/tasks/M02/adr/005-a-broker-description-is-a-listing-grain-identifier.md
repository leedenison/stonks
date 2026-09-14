# A broker description is a listing grain identifier

A broker's description of an instrument is stated alongside a currency, and the
description together with the currency is what the broker uses to name the line. The
broker description identifier type is therefore listing grain.

A stated key whose broker description names an existing listing but states a different
currency or asset class contradicts that listing, as any identifier contradicts the
subject it names when the two disagree. It is not a second listing of the same
instrument and it is not silently matched.
