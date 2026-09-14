# Marshalling happens in the client

A broker's export has to be translated into the neutral format somewhere. The client is a
browser application, so the export is read and translated in the browser and the neutral
format is what the API accepts.

The server then interprets nothing broker specific: it validates one format, and the
knowledge of each broker's conventions lives in a marshaller in front of the API. The
export itself never reaches the server.

## Consequences

The neutral format is a proto message and is the contract every channel speaks. A channel
that is not a browser brings its own marshaller in front of the same RPC.
