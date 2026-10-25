import { useEffect, useRef } from "react";
import type { ReactNode } from "react";
import "./Dialog.css";

interface DialogProps {
  title: string;
  onClose: () => void;
  // False while whatever the dialog set going is still out. The Cancel button
  // inside is off then, and Escape and the backdrop are held to the same
  // answer, or a dialog could be shut on a save it is about to report on.
  dismissable?: boolean;
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
//
// Holding Escape off is a request the browser may overrule — Chrome lets a
// second Escape through regardless — so onClose still runs whenever the
// dialog does close, rather than leaving an invisible one mounted.
export default function Dialog({
  title,
  onClose,
  dismissable = true,
  children,
}: DialogProps) {
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
      onCancel={(e) => {
        if (!dismissable) e.preventDefault();
      }}
      onClick={(e) => {
        if (dismissable && e.target === ref.current) onClose();
      }}
    >
      <h2 className="dialog-title">{title}</h2>
      {children}
    </dialog>
  );
}
