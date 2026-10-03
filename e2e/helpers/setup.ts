import { adminClient } from "./api";
import { closeRedis, deleteSession, seedSession } from "./auth";
import { closeDB, deleteUser, seedDatasource, seedUser } from "./db";

const name = "openfigi";
const endpoint = "http://vcrproxy:8080";
// A placeholder: the proxy strips the header before anything leaves the stack.
// Holding one lifts the service's call rate to the provider's authenticated
// rate, so the suite's fetches are not spaced by seconds.
const credential = "e2e";

// Names the proxy as the one datasource. The registry reads the table only
// when a row changes through the admin service, so the values are also saved
// through it.
export default async function setup(): Promise<void> {
  try {
    await seedDatasource(name, endpoint, credential);
    const admin = await seedUser("admin");
    const session = await seedSession(admin);
    try {
      await adminClient(session).updateDatasource({
        name,
        enabled: true,
        endpoint,
        credential,
      });
    } finally {
      await deleteSession(session);
      await deleteUser(admin.id);
    }
  } finally {
    await closeDB();
    await closeRedis();
  }
}
