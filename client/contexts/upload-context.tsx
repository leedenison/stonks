"use client";

import { createContext, type ReactNode, useContext, useState } from "react";

// What the upload dialog is showing. Each opening has its own key, so the
// dialog starts afresh from the first stage; a file given at opening skips
// the choosing stage.
export type UploadState = {
  open: boolean;
  file?: File;
  key: number;
};

type UploadValue = {
  state: UploadState;
  open: (file?: File) => void;
  close: () => void;
};

const UploadContext = createContext<UploadValue | null>(null);

export function UploadProvider({ children }: { children: ReactNode }) {
  const [state, setState] = useState<UploadState>({ open: false, key: 0 });
  const value: UploadValue = {
    state,
    open: (file) => setState((s) => ({ open: true, file, key: s.key + 1 })),
    close: () => setState((s) => ({ ...s, open: false, file: undefined })),
  };
  return (
    <UploadContext.Provider value={value}>{children}</UploadContext.Provider>
  );
}

export function useUpload(): UploadValue {
  const ctx = useContext(UploadContext);
  if (!ctx) {
    throw new Error("useUpload called outside UploadProvider");
  }
  return ctx;
}
