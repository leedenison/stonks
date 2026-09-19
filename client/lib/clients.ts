import { createClient, type Transport } from "@connectrpc/connect";
import { AuthService } from "@/gen/auth/v1/auth_pb";
import { HoldingService } from "@/gen/holding/v1/holding_pb";
import { RunService } from "@/gen/run/v1/run_pb";
import { StatementService } from "@/gen/statement/v1/statement_pb";

// newClients returns one typed client per service over transport.
export function newClients(transport: Transport) {
  return {
    auth: createClient(AuthService, transport),
    holding: createClient(HoldingService, transport),
    run: createClient(RunService, transport),
    statement: createClient(StatementService, transport),
  };
}

export type Clients = ReturnType<typeof newClients>;
