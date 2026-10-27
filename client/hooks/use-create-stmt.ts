"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useClients } from "@/contexts/clients-context";
import type { Stmt } from "@/gen/stmt/v1/stmt_pb";
import { qk } from "@/lib/query-keys";

// useCreateStmt starts a statement's run. The list of statements is
// invalidated on success, so the pending run appears at once.
export function useCreateStmt() {
  const { stmt } = useClients();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (s: Stmt) => stmt.createStmt({ stmt: s }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: qk.stmts() }),
  });
}
