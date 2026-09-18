"use client";

import { Check, ChevronDown } from "lucide-react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import {
  createContext,
  type ReactNode,
  useContext,
  useEffect,
  useRef,
  useState,
} from "react";

const CloseContext = createContext<() => void>(() => {});

const itemClass =
  "flex w-full items-center gap-2 px-4 py-2 text-left text-sm text-text-primary transition-colors hover:bg-primary-light/15";

// Menu is a dropdown under its trigger. It closes on Escape, on a click
// outside, when an item is chosen and on navigation: the open state is the
// route it was opened on, so a route change closes it by derivation.
export function Menu({
  label,
  testId,
  panelTestId,
  className = "",
  children,
}: {
  label: ReactNode;
  testId?: string;
  panelTestId?: string;
  className?: string;
  children: ReactNode;
}) {
  const pathname = usePathname();
  const ref = useRef<HTMLDivElement>(null);
  const [openedAt, setOpenedAt] = useState<string | null>(null);
  const open = openedAt === pathname;

  useEffect(() => {
    if (!open) {
      return;
    }
    const onPointer = (e: MouseEvent) => {
      if (!ref.current?.contains(e.target as Node)) {
        setOpenedAt(null);
      }
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        setOpenedAt(null);
      }
    };
    document.addEventListener("mousedown", onPointer);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onPointer);
      document.removeEventListener("keydown", onKey);
    };
  }, [open]);

  return (
    <div ref={ref} className="relative">
      <button
        type="button"
        data-testid={testId}
        aria-haspopup="menu"
        aria-expanded={open}
        onClick={() => setOpenedAt(open ? null : pathname)}
        className={`flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-sm font-medium transition-colors ${className}`}
      >
        {label}
        <ChevronDown
          aria-hidden
          className={`h-4 w-4 transition-transform ${open ? "rotate-180" : ""}`}
        />
      </button>
      {open && (
        <div
          role="menu"
          data-testid={panelTestId}
          className="absolute top-full right-0 z-50 mt-1 w-56 overflow-hidden rounded-lg bg-surface py-1 shadow-lg ring-1 ring-border"
        >
          <CloseContext.Provider value={() => setOpenedAt(null)}>
            {children}
          </CloseContext.Provider>
        </div>
      )}
    </div>
  );
}

export function MenuHeader({ children }: { children: ReactNode }) {
  return (
    <div className="mb-1 truncate border-b border-border px-4 py-2 text-sm font-medium text-text-primary">
      {children}
    </div>
  );
}

export function MenuLink({
  href,
  testId,
  children,
}: {
  href: string;
  testId?: string;
  children: ReactNode;
}) {
  const close = useContext(CloseContext);
  return (
    <Link
      role="menuitem"
      href={href}
      data-testid={testId}
      onClick={close}
      className={itemClass}
    >
      {children}
    </Link>
  );
}

// MenuItem with selected set is one of a radio group and shows its state.
export function MenuItem({
  onSelect,
  selected,
  testId,
  children,
}: {
  onSelect: () => void;
  selected?: boolean;
  testId?: string;
  children: ReactNode;
}) {
  const close = useContext(CloseContext);
  const radio = selected !== undefined;
  return (
    <button
      type="button"
      role={radio ? "menuitemradio" : "menuitem"}
      aria-checked={radio ? selected : undefined}
      data-testid={testId}
      onClick={() => {
        close();
        onSelect();
      }}
      className={itemClass}
    >
      {radio && (
        <Check
          aria-hidden
          className={`h-4 w-4 ${selected ? "" : "invisible"}`}
        />
      )}
      {children}
    </button>
  );
}

export function MenuSeparator() {
  return <div role="separator" className="my-1 border-t border-border" />;
}
