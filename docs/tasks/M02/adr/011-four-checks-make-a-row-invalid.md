# Four checks make a row invalid

A row is invalid when it:

- states an identifier whose type its asset class does not admit, such as an ISIN on a
  cash leg or a currency identifier on anything but cash;
- states an asset class outside the vocabulary;
- states an order date outside the claimed period;
- states an order date after today.

The claimed period is a range of order dates. Replacement is keyed on order date, and a
settlement date may fall outside the period on either side.

An invalid row is rejected on its own and recorded as an item of the run, so the
validating interceptor cannot be what rejects it: a constraint on a row field would fail
the whole upload. Row fields carry no protovalidate constraint, the interceptor validates
the envelope, and the handler checks each row.
