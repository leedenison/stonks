# One vcrproxy serves every provider

The e2e stack runs one vcrproxy for OpenFIGI, Massive and EODHD. The proxy routes a
request by the hostname in its URL. Each datasource's endpoint in the e2e setup is a
hostname of its own, such as `openfigi.vcr`, and the compose network aliases every one of
them to the proxy's container.

The proxy is primed with a file that maps each hostname to its provider's settings:

- the upstream, which receives the requests that recording forwards;
- the recorded cassette and the authored cassette;
- the credential to strip before matching or forwarding, which is a header for OpenFIGI
  and a query parameter for Massive and EODHD;
- the least time between upstream calls when recording.

Each provider keeps its own cassettes, so one provider is re-recorded without touching
the others. A request to a hostname the file does not name fails.
