import type { PointerEvent } from "react";

// DragHandle is the grip an item of a useDragReorder list is moved by. With
// focus, the arrow keys move it a place at a time.
//
// busy is marked rather than disabling it: a disabled button drops its focus,
// and a keyboard user moving one several places would lose the handle after
// the first.
export default function DragHandle({
  label,
  title,
  place,
  count,
  busy,
  onGrab,
  onMove,
}: {
  label: string;
  title: string;
  // 0-based.
  place: number;
  count: number;
  busy: boolean;
  onGrab: (e: PointerEvent<HTMLButtonElement>) => void;
  onMove: (by: -1 | 1) => void;
}) {
  return (
    <button
      type="button"
      className="drag-handle"
      aria-label={label}
      title={title}
      disabled={count < 2}
      aria-disabled={busy || undefined}
      onPointerDown={(e) => {
        if (!busy) onGrab(e);
      }}
      onKeyDown={(e) => {
        const by = e.key === "ArrowUp" ? -1 : e.key === "ArrowDown" ? 1 : 0;
        if (by === 0) return;
        e.preventDefault();
        if (!busy && place + by >= 0 && place + by < count) onMove(by);
      }}
    >
      <svg viewBox="0 0 10 16" width="10" height="16" aria-hidden="true">
        {[3, 8, 13].map((y) =>
          [2.5, 7.5].map((x) => (
            <circle key={`${x}-${y}`} cx={x} cy={y} r="1.5" />
          )),
        )}
      </svg>
    </button>
  );
}
