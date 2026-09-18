"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useClients } from "@/contexts/clients-context";
import type { Statement } from "@/gen/statement/v1/statement_pb";
import { qk } from "@/lib/query-keys";

// useCreateStatement starts a statement's run. The list of statements is
// invalidated on success, so the pending run appears at once.
export function useCreateStatement() {
  const { statement } = useClients();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (s: Statement) => statement.createStatement({ statement: s }),
    onSuccess: () =>
      queryClient.invalidateQueries({ queryKey: qk.statements() }),
  });
}
