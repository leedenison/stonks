"use client";

import { type DragEvent, useState } from "react";

// useDropTarget returns the props of an element that takes a dropped file:
// data-over while a drag is over it, for a style to mark it, and the
// handlers that hand the dropped file to onFile.
export function useDropTarget(onFile: (file: File) => void) {
  const [over, setOver] = useState(false);
  return {
    "data-over": over || undefined,
    onDragOver: (e: DragEvent) => {
      e.preventDefault();
      setOver(true);
    },
    onDragLeave: () => setOver(false),
    onDrop: (e: DragEvent) => {
      e.preventDefault();
      setOver(false);
      const file = e.dataTransfer.files[0];
      if (file) {
        onFile(file);
      }
    },
  };
}
