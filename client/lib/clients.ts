import { createClient, type Transport } from "@connectrpc/connect";
import { BlockService } from "@/gen/admin/block/v1/block_pb";
import { DatasourceService } from "@/gen/admin/datasource/v1/datasource_pb";
import { InstrumentService as AdminInstrumentService } from "@/gen/admin/instrument/v1/instrument_pb";
import { ExtendValidityService } from "@/gen/admin/extend_validity/v1/extend_validity_pb";
import { ReplayService } from "@/gen/admin/replay/v1/replay_pb";
import { RunService as AdminRunService } from "@/gen/admin/run/v1/run_pb";
import { AuthService } from "@/gen/auth/v1/auth_pb";
import { HoldingService } from "@/gen/holding/v1/holding_pb";
import { RunService } from "@/gen/run/v1/run_pb";
import { StatementService } from "@/gen/statement/v1/statement_pb";

// newClients returns one typed client per service over transport.
export function newClients(transport: Transport) {
  return {
    adminInstrument: createClient(AdminInstrumentService, transport),
    adminRun: createClient(AdminRunService, transport),
    auth: createClient(AuthService, transport),
    block: createClient(BlockService, transport),
    datasource: createClient(DatasourceService, transport),
    holding: createClient(HoldingService, transport),
    extendValidity: createClient(ExtendValidityService, transport),
    replay: createClient(ReplayService, transport),
    run: createClient(RunService, transport),
    statement: createClient(StatementService, transport),
  };
}

export type Clients = ReturnType<typeof newClients>;
