# End of day prices feed charts, and the display currency has no effect yet

The holdings page omits valuation in this milestone. A holding is valued from a current
price, and current prices come from a part of the system that D-QUOTES defers. End of day
prices describe completed days, so they serve history.

The one reader of prices in this milestone is a chart for an administrator on the
instrument details page. It draws each listing of the instrument, and it draws the rate
of a currency on the details page of that currency. See
[001](001-a-price-attaches-to-a-listing-and-an-fx-rate-is-a-price.md). A chart of the
valuation of a holding over time is deferred with the valuation itself.

A user still selects a display currency in this milestone. Each user has one, and it is
USD until the user selects another. The profile page offers the family currencies alone,
so GBX is not offered. The selection starts the rate fetch, so the rates are in place
when a valuation first reads them. The selection changes the screen only once a valuation
reads it.

Inferring the default from the currency of most of a user's holdings was the alternative.
It would change as the user uploads statements, and one selection corrects a fixed
default.
