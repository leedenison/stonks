"use client";

import type { UseQueryResult } from "@tanstack/react-query";
import { useClients } from "@/contexts/clients-context";
import type { GetStatementResponse } from "@/gen/statement/v1/statement_pb";
import { qk } from "@/lib/query-keys";
import { useAuthedQuery } from "./use-authed-query";

// useStatement reads one statement with every rejected row. It does not
// poll: the items are the whole record, and a page that needs to follow the
// run reads it through useRun and refetches this once the run is terminal.
export function useStatement(id: string): UseQueryResult<GetStatementResponse> {
  const { statement } = useClients();
  return useAuthedQuery({
    queryKey: qk.statement(id),
    queryFn: () => statement.getStatement({ runId: id }),
  });
}
