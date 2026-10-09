import type { Identifier } from "../gen/type/v1/type_pb";
import { store } from "./redis";

// The key format is the contract the server's market package states: a
// fixed prefix, then the datasource, the kind, the identifier sent and the
// parameters the integration derives from the request, in that order.
const prefix = "stonks:fetch:";

// fetchCacheKey is the cache key of one request.
export function fetchCacheKey(
  datasource: string,
  kind: string,
  sent: Pick<Identifier, "domain" | "value"> & { type: string },
  params: string[] = [],
): string {
  return (
    prefix +
    [datasource, kind, sent.type, sent.domain, sent.value, ...params].join(":")
  );
}

// dropFetchCache removes one cached answer, so the next fetch of the request
// misses. A spec drops only the entries its own fixture produced.
export async function dropFetchCache(key: string): Promise<void> {
  await store().del(key);
}
