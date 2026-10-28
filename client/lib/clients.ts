import { createClient, type Transport } from "@connectrpc/connect";
import { BlockService } from "@/gen/admin/block/v1/block_pb";
import { CoverageService } from "@/gen/admin/coverage/v1/coverage_pb";
import { DatasourceService } from "@/gen/admin/datasource/v1/datasource_pb";
import { InstrService as AdminInstrService } from "@/gen/admin/instr/v1/instr_pb";
import { ReassertTickersService } from "@/gen/admin/reassert_tickers/v1/reassert_tickers_pb";
import { RebuildTickerValidityService } from "@/gen/admin/rebuild_ticker_validity/v1/rebuild_ticker_validity_pb";
import { RefreshCorpActsService } from "@/gen/admin/refresh_corp_acts/v1/refresh_corp_acts_pb";
import { ReplayRunService } from "@/gen/admin/replay_run/v1/replay_run_pb";
import { RunService as AdminRunService } from "@/gen/admin/run/v1/run_pb";
import { AuthService } from "@/gen/auth/v1/auth_pb";
import { HoldingService } from "@/gen/holding/v1/holding_pb";
import { RunService } from "@/gen/run/v1/run_pb";
import { StmtService } from "@/gen/stmt/v1/stmt_pb";

// newClients returns one typed client per service over transport.
export function newClients(transport: Transport) {
  return {
    adminInstr: createClient(AdminInstrService, transport),
    adminRun: createClient(AdminRunService, transport),
    auth: createClient(AuthService, transport),
    block: createClient(BlockService, transport),
    coverage: createClient(CoverageService, transport),
    datasource: createClient(DatasourceService, transport),
    holding: createClient(HoldingService, transport),
    reassertTickers: createClient(ReassertTickersService, transport),
    rebuildTickerValidity: createClient(
      RebuildTickerValidityService,
      transport,
    ),
    refreshCorpActs: createClient(RefreshCorpActsService, transport),
    replayRun: createClient(ReplayRunService, transport),
    run: createClient(RunService, transport),
    stmt: createClient(StmtService, transport),
  };
}

export type Clients = ReturnType<typeof newClients>;
