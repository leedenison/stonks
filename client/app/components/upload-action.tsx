"use client";

import { Upload } from "lucide-react";
import { useUpload } from "@/contexts/upload-context";
import { Button } from "./button";

// UploadAction opens the upload dialog from a page's action bar.
export function UploadAction() {
  const upload = useUpload();
  return (
    <Button
      variant="text"
      data-testid="upload-statement"
      onClick={() => upload.open()}
    >
      <Upload aria-hidden className="h-4 w-4" />
      Upload statement
    </Button>
  );
}
