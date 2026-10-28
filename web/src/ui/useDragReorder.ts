import {
  useLayoutEffect,
  useRef,
  useState,
  type CSSProperties,
  type PointerEvent,
} from "react";
import "./dragReorder.css";

// Where an item stood in the list when a drag began, in the list's own
// coordinates.
interface Slot {
  top: number;
  height: number;
}

// A drag in flight: which item, the order it would drop in, where every item
// stood when it began, and how far the pointer has come since.
interface Drag {
  id: string;
  order: string[];
  slots: Map<string, Slot>;
  from: number;
  dy: number;
}

// useDragReorder is a list put in order by hand: an item is dragged by its
// handle with a mouse or a finger, or moved a place at a time with the arrow
// keys once the handle has focus.
//
// The list keeps its order while a drag lasts: the held item follows the
// pointer, and the others slide out of its way to where the order it would
// drop in puts them. Only the drop calls onReorder, with every id in the new
// order, and only then does the list take it — from the caller, once it holds
// the order it wrote.
export function useDragReorder(
  ids: string[] | null,
  onReorder: (order: string[]) => void,
) {
  const [drag, setDrag] = useState<Drag | null>(null);
  // The list captures the pointer rather than the handle, so a drag survives
  // whatever happens to the handle's own item under it.
  const list = useRef<HTMLOListElement>(null);
  const items = useRef(new Map<string, HTMLLIElement>());
  // Where the dropped item was on screen as it was let go, so it can glide
  // into its slot rather than leap there.
  const settling = useRef<{ id: string; top: number } | null>(null);

  // After a drop has been drawn: start the dropped item where the pointer left
  // it and slide it home. A layout effect, so it never shows a frame in its
  // slot first.
  const key = ids?.join(",");
  useLayoutEffect(() => {
    const dropped = settling.current;
    if (!dropped || drag) return;
    settling.current = null;
    const el = items.current.get(dropped.id);
    if (!el || typeof el.animate !== "function") return;
    if (window.matchMedia?.("(prefers-reduced-motion: reduce)").matches) return;
    const dy = dropped.top - el.getBoundingClientRect().top;
    if (Math.abs(dy) < 1) return;
    el.animate([{ transform: `translateY(${dy}px)` }, { transform: "none" }], {
      duration: 150,
      easing: "ease",
    });
  }, [drag, key]);

  // The list's own coordinates, which the items' slides do not move.
  function pointerY(e: PointerEvent) {
    return e.clientY - (list.current?.getBoundingClientRect().top ?? 0);
  }

  function grab(id: string, e: PointerEvent<HTMLElement>) {
    if (e.button !== 0 || !ids || !list.current) return;
    // Otherwise a drag on a phone scrolls the page, and on a desk selects text.
    e.preventDefault();
    list.current.setPointerCapture(e.pointerId);
    const top = list.current.getBoundingClientRect().top;
    const slots = new Map<string, Slot>();
    for (const i of ids) {
      const box = items.current.get(i)?.getBoundingClientRect();
      if (box) slots.set(i, { top: box.top - top, height: box.height });
    }
    setDrag({ id, order: [...ids], slots, from: pointerY(e), dy: 0 });
  }

  // Where the held item would land: after every other one whose middle its own
  // middle has passed. Both middles are where things stood when the drag
  // began, so the line it crosses going down is the line it crosses coming
  // back, and the slides cannot move it.
  function dragOver(e: PointerEvent<HTMLOListElement>) {
    if (!drag) return;
    const dy = pointerY(e) - drag.from;
    const held = drag.slots.get(drag.id);
    const middle = (held ? held.top + held.height / 2 : 0) + dy;
    const others = drag.order.filter((id) => id !== drag.id);
    const passed = others.filter((id) => {
      const slot = drag.slots.get(id);
      return slot !== undefined && slot.top + slot.height / 2 < middle;
    }).length;
    const order = [
      ...others.slice(0, passed),
      drag.id,
      ...others.slice(passed),
    ];
    setDrag({ ...drag, order, dy });
  }

  function drop() {
    if (!drag || !ids) return;
    const held = items.current.get(drag.id);
    if (held) {
      settling.current = { id: drag.id, top: held.getBoundingClientRect().top };
    }
    setDrag(null);
    if (drag.order.some((id, i) => id !== ids[i])) onReorder(drag.order);
  }

  function moveBy(id: string, by: -1 | 1) {
    if (!ids) return;
    const order = [...ids];
    const from = order.indexOf(id);
    const to = from + by;
    if (from < 0 || to < 0 || to >= order.length) return;
    [order[from], order[to]] = [order[to], order[from]];
    onReorder(order);
  }

  // Where each item is drawn while a drag lasts: the held one wherever the
  // pointer has taken it, and the rest in the slots the order would give them,
  // stacked from the top with the gaps between them as they were.
  const slides = new Map<string, number>();
  if (drag && ids) {
    const first = drag.slots.get(ids[0] ?? "");
    const second = drag.slots.get(ids[1] ?? "");
    const gap = first && second ? second.top - (first.top + first.height) : 0;
    let y = first?.top ?? 0;
    for (const id of drag.order) {
      const slot = drag.slots.get(id);
      if (!slot) continue;
      slides.set(id, id === drag.id ? drag.dy : y - slot.top);
      y += slot.height + gap;
    }
  }

  return {
    // The id being dragged, if any.
    held: drag?.id ?? null,
    // Where an item would be, 0-based, in the order the drag would leave the
    // list — so a number drawn on it can follow the drag.
    placeOf: (id: string) =>
      drag ? drag.order.indexOf(id) : (ids?.indexOf(id) ?? -1),
    listProps: {
      ref: list,
      className: drag ? "drag-list drag-list--dragging" : "drag-list",
      onPointerMove: dragOver,
      onPointerUp: drop,
      onPointerCancel: () => setDrag(null),
    },
    itemProps: (id: string) => ({
      ref: (el: HTMLLIElement | null) => {
        if (el) items.current.set(id, el);
        else items.current.delete(id);
      },
      className: drag?.id === id ? "drag-held" : undefined,
      style: (slides.has(id)
        ? { transform: `translateY(${slides.get(id)}px)` }
        : undefined) as CSSProperties | undefined,
    }),
    grab,
    moveBy,
  };
}
