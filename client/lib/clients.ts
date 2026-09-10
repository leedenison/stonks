import { createClient, type Transport } from "@connectrpc/connect";
import { AuthService } from "@/gen/auth/v1/auth_pb";

// newClients returns one typed client per service over transport.
export function newClients(transport: Transport) {
  return {
    auth: createClient(AuthService, transport),
  };
}

export type Clients = ReturnType<typeof newClients>;
