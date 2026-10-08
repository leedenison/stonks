import type { JsonObject } from "@bufbuild/protobuf";
import { adminClient } from "./api";
import { closeRedis, deleteSession, seedSession } from "./auth";
import { closeDB, deleteUsers, seedDatasource, seedUser } from "./db";

// Each datasource is the proxy under the provider's hostname. The credential
// is a placeholder that the proxy strips before anything leaves the stack.
// With a credential, the service calls OpenFIGI at its authenticated rate.
// Massive's starter plan has no rate limit. The suite's fetches are therefore
// not spaced by seconds.
const datasources: {
  name: string;
  precedence: number;
  endpoint: string;
  config: JsonObject;
}[] = [
  {
    name: "openfigi",
    precedence: 1,
    endpoint: "http://openfigi.vcr:8080",
    config: {},
  },
  {
    name: "massive",
    precedence: 2,
    endpoint: "http://massive.vcr:8080",
    config: {
      plan: "starter",
      plans: { basic: { perMinute: 5 }, starter: {} },
    },
  },
];
const credential = "e2e";

// Names the proxy as every datasource. The registry reads the table only
// when a row changes through the admin service, so the values are also saved
// through it.
export default async function setup(): Promise<void> {
  try {
    for (const d of datasources) {
      await seedDatasource({ ...d, credential });
    }
    const admin = await seedUser("admin");
    const session = await seedSession(admin);
    try {
      for (const d of datasources) {
        await adminClient(session).updateDatasource({
          name: d.name,
          enabled: true,
          endpoint: d.endpoint,
          credential,
          config: d.config,
        });
      }
    } finally {
      await deleteSession(session);
      await deleteUsers([admin.id]);
    }
  } finally {
    await closeDB();
    await closeRedis();
  }
}
