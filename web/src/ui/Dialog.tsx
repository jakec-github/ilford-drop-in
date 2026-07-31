import { useEffect, useRef } from "react";
import type { ReactNode } from "react";
import "./Dialog.css";

interface DialogProps {
  title: string;
  onClose: () => void;
  children: ReactNode;
}

// A modal dialog, mounted only while it is open.
//
// The native <dialog> in modal mode brings the focus trap, the backdrop, the
// inert page behind it and Escape-to-close with no code of ours; the effect is
// only here because modal state is imperative and cannot be set in JSX. The
// click handler adds the one thing the platform leaves out — closing on a
// backdrop click, which lands on the dialog element itself rather than on
// anything inside it.
export default function Dialog({ title, onClose, children }: DialogProps) {
  const ref = useRef<HTMLDialogElement>(null);

  useEffect(() => {
    ref.current?.showModal();
  }, []);

  return (
    <dialog
      ref={ref}
      className="dialog"
      aria-label={title}
      onClose={onClose}
      onClick={(e) => {
        if (e.target === ref.current) onClose();
      }}
    >
      <h2 className="dialog-title">{title}</h2>
      {children}
    </dialog>
  );
}
