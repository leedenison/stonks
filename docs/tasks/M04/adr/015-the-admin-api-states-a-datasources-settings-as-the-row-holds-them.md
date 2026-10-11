# The admin API states a datasource's settings as the row holds them

An administrator checks which key and address each datasource uses on the datasources
admin page, and edits either in place. The admin API therefore returns the credential,
and the page shows it as plain text. The admin pages are open to administrators alone,
and who may read a credential is a later question.

A datasource carries two addresses. The endpoint is what the row sets, empty for the
provider's default. The default endpoint is the address the integration calls when the
row sets none, and is empty when the build carries no integration of the name. The page
shows the endpoint, or the default where the row sets none, and the dialog shows the
default as the endpoint field's placeholder. Returning one effective address would make
the dialog write the default into the row on every save, so the row would stop following
a build whose default changes.

An update states the whole row. The request carries the state, the endpoint, the
credential and the config as plain values, and the service writes them as given. An
empty endpoint or credential is stored as NULL. The page holds every setting, so
no caller needs to leave a field unchanged. The toggle sends the row's settings
alongside the new state.

The default lives beside the factory, because a disabled Massive or EODHD row has no
integration to ask: their factories fail without a credential.
