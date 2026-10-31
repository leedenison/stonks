"use client";

import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef } from "react";
import { qk } from "@/lib/query-keys";

// useRunSettled refreshes the alerts when live goes from true to false
// while the caller is mounted, since a run that ends can raise or settle an
// alert. If the hook first sees a run settled, it does nothing.
export function useRunSettled(live: boolean | undefined) {
  const queryClient = useQueryClient();
  const was = useRef<boolean | undefined>(undefined);
  useEffect(() => {
    const settled = was.current === true && live === false;
    was.current = live;
    if (settled) {
      queryClient.invalidateQueries({ queryKey: qk.alertsAll() });
    }
  }, [live, queryClient]);
}
