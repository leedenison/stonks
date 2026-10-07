# Fetches are cached for an hour

A datasource's answer is cached in Redis for an hour after a served fetch, for every
fetch, in a resolution run or a synchronous resolution. A fetch reads the cache first and
calls the datasource only on a miss. A user most often inspects a key right after its
upload resolved, and a datasource's answer is taken to hold for an hour. So the
candidates a user lists, the candidates they confirm and the answer the run used are one
answer, fetched once.

The cache key is the request as the integration sends it: the datasource, the kind of
data, the identifier sent, and any parameter the integration derives from the stated
key, such as the currency that OpenFIGI uses as a filter. Answers are system facts, so
the cache is shared by every user. Only a served answer is cached, including an empty
one. An administrator's change to a datasource's row drops that datasource's entries.

A cache hit records a served fetch key in its own fetch, as a call does, but with zero
attempts. The moment the fetch key records can trail the datasource's answer by up to an
hour. That is finer than the dates that record an identifier's validity.

## Consequences

The e2e suite runs its specs in parallel against one Redis. Each spec states a different
security, so the cache keys of two specs stay distinct, and the suite leaves the cache
intact between tests. A spec that needs a miss within one test deletes its own entries
through an e2e helper. The cache key format is therefore a contract, like the session
record's key. It is a fixed prefix followed by the request's fields in a fixed order. The
service reads the hour from its environment, and the suite keeps the default.
