import { useEffect, useRef, useState } from "react";
import type { KeyboardEvent, PointerEvent } from "react";
import type { RuleKind } from "../types";

// RuleValueInput sets the one number a rule carries, with the control its kind
// asks for (RuleKind.input). The library names the control, not the rule, so a
// kind arriving with an input this build already knows needs no change here,
// and one arriving with an input it does not know still gets a number box.
//
// Every control reports each value as it moves. Saving them is the card's
// business, which waits for the movement to stop.
export default function RuleValueInput({
  kind,
  value,
  disabled,
  onChange,
}: {
  kind: RuleKind;
  value: number;
  disabled: boolean;
  onChange: (value: number) => void;
}) {
  const min = kind.minValue ?? 0;
  const max = kind.maxValue ?? Number.MAX_SAFE_INTEGER;
  const label = kind.valueLabel ?? "Value";
  const props = { label, value, min, max, disabled, onChange };
  switch (kind.input) {
    case "percent":
      return <PercentRing {...props} />;
    case "stepper":
      return <Stepper {...props} />;
    case "count":
      return <GrowingSlider {...props} />;
    default:
      return <NumberBox {...props} />;
  }
}

interface ControlProps {
  label: string;
  value: number;
  min: number;
  max: number;
  disabled: boolean;
  onChange: (value: number) => void;
}

const clamp = (n: number, min: number, max: number) =>
  Math.min(max, Math.max(min, n));

// TypedNumber is a figure that can be clicked and typed over. Committed on
// Enter or on leaving it, clamped to what the kind accepts; Escape puts back
// what was there.
function TypedNumber({
  label,
  value,
  min,
  max,
  disabled,
  onChange,
  suffix,
  className,
}: ControlProps & { suffix?: string; className?: string }) {
  const [text, setText] = useState<string | null>(null);
  const shown = text ?? String(value);
  function commit() {
    if (text === null) return;
    const n = Number.parseInt(text, 10);
    setText(null);
    if (Number.isFinite(n)) onChange(clamp(n, min, max));
  }
  return (
    <span className={`typed-number ${className ?? ""}`}>
      <input
        aria-label={label}
        inputMode="numeric"
        disabled={disabled}
        value={shown}
        size={Math.max(1, shown.length)}
        onFocus={(e) => e.currentTarget.select()}
        onChange={(e) => setText(e.target.value.replace(/[^0-9]/g, ""))}
        onBlur={commit}
        onKeyDown={(e) => {
          if (e.key === "Enter") e.currentTarget.blur();
          if (e.key === "Escape") {
            setText(null);
            e.currentTarget.blur();
          }
        }}
      />
      {suffix && <span className="typed-number-suffix">{suffix}</span>}
    </span>
  );
}

// The ring's geometry, in the SVG's own units.
const RING_RADIUS = 44;
const RING_LENGTH = 2 * Math.PI * RING_RADIUS;

// PercentRing is a share drawn as how much of a circle it fills. Dragged round,
// it moves in fives; the arrow keys move it by one and Page Up/Down by ten.
function PercentRing({
  label,
  value,
  min,
  max,
  disabled,
  onChange,
}: ControlProps) {
  const ring = useRef<SVGSVGElement>(null);
  const [dragging, setDragging] = useState(false);

  function fromPointer(e: PointerEvent<SVGSVGElement>) {
    const box = ring.current?.getBoundingClientRect();
    if (!box) return;
    const dx = e.clientX - (box.left + box.width / 2);
    const dy = e.clientY - (box.top + box.height / 2);
    let turn = Math.atan2(dx, -dy) / (2 * Math.PI);
    if (turn < 0) turn += 1;
    // The top of the ring is both 0 and 100; a drag ending there means full.
    const snapped = Math.round(turn * 20) * 5 || 100;
    onChange(clamp(snapped, min, max));
  }

  function onKeyDown(e: KeyboardEvent<SVGSVGElement>) {
    const step: Record<string, number> = {
      ArrowUp: 1,
      ArrowRight: 1,
      ArrowDown: -1,
      ArrowLeft: -1,
      PageUp: 10,
      PageDown: -10,
    };
    if (e.key === "Home") onChange(min);
    else if (e.key === "End") onChange(max);
    else if (e.key in step) onChange(clamp(value + step[e.key], min, max));
    else return;
    e.preventDefault();
  }

  const share = clamp(value, 0, 100) / 100;
  const angle = share * 2 * Math.PI;
  return (
    <div className="percent-ring">
      <svg
        ref={ring}
        viewBox="0 0 110 110"
        className={
          dragging ? "percent-ring-dial dragging" : "percent-ring-dial"
        }
        role="slider"
        aria-label={label}
        aria-valuemin={min}
        aria-valuemax={max}
        aria-valuenow={value}
        aria-valuetext={`${value}%`}
        aria-disabled={disabled || undefined}
        tabIndex={disabled ? -1 : 0}
        onKeyDown={disabled ? undefined : onKeyDown}
        onPointerDown={(e) => {
          if (disabled) return;
          e.currentTarget.setPointerCapture(e.pointerId);
          setDragging(true);
          fromPointer(e);
        }}
        onPointerMove={(e) => dragging && fromPointer(e)}
        onPointerUp={() => setDragging(false)}
        onPointerCancel={() => setDragging(false)}
      >
        <circle
          className="percent-ring-track"
          cx="55"
          cy="55"
          r={RING_RADIUS}
        />
        <circle
          className="percent-ring-fill"
          cx="55"
          cy="55"
          r={RING_RADIUS}
          strokeDasharray={RING_LENGTH}
          strokeDashoffset={RING_LENGTH * (1 - share)}
          transform="rotate(-90 55 55)"
        />
        <circle
          className="percent-ring-knob"
          cx={55 + RING_RADIUS * Math.sin(angle)}
          cy={55 - RING_RADIUS * Math.cos(angle)}
          r="7"
        />
      </svg>
      <TypedNumber
        label={`${label}, typed`}
        value={value}
        min={min}
        max={max}
        disabled={disabled}
        onChange={onChange}
        suffix="%"
        className="percent-ring-figure"
      />
    </div>
  );
}

// useRepeat makes a button that keeps going while held: one step on the press,
// then after a pause, faster and faster.
function useRepeat(step: () => void) {
  const timer = useRef<number | null>(null);
  const latest = useRef(step);
  useEffect(() => {
    latest.current = step;
  });
  const stop = () => {
    if (timer.current !== null) window.clearTimeout(timer.current);
    timer.current = null;
  };
  useEffect(() => stop, []);
  return {
    onPointerDown: () => {
      stop();
      const go = (delay: number) => {
        timer.current = window.setTimeout(() => {
          latest.current();
          go(Math.max(40, delay * 0.8));
        }, delay);
      };
      go(400);
    },
    onPointerUp: stop,
    onPointerLeave: stop,
    onPointerCancel: stop,
  };
}

// How many of a gap's shifts the strip draws before it gives up drawing each
// one and draws the stretch as a line instead.
const STRIP_LIMIT = 8;

// Stepper is a count nudged by one, with no ceiling short of what the kind
// accepts. The strip under it shows the gap as worked and free shifts.
function Stepper({ label, value, min, max, disabled, onChange }: ControlProps) {
  const down = useRepeat(() => onChange(clamp(value - 1, min, max)));
  const up = useRepeat(() => onChange(clamp(value + 1, min, max)));
  return (
    <div className="rule-stepper">
      <div className="rule-stepper-row">
        <button
          type="button"
          className="rule-stepper-button"
          aria-label={`Fewer: ${label}`}
          disabled={disabled || value <= min}
          onClick={() => onChange(clamp(value - 1, min, max))}
          {...down}
        >
          −
        </button>
        <TypedNumber
          label={label}
          value={value}
          min={min}
          max={max}
          disabled={disabled}
          onChange={onChange}
          className="rule-figure"
        />
        <button
          type="button"
          className="rule-stepper-button"
          aria-label={`More: ${label}`}
          disabled={disabled || value >= max}
          onClick={() => onChange(clamp(value + 1, min, max))}
          {...up}
        >
          +
        </button>
        <span className="rule-stepper-label" aria-hidden="true">
          {label.toLowerCase()}
        </span>
      </div>
      <GapStrip gap={value} />
    </div>
  );
}

// GapStrip draws a gap as the shifts it spans: worked, then this many free,
// then worked again. Past STRIP_LIMIT it is a line labelled with the count.
function GapStrip({ gap }: { gap: number }) {
  if (gap > STRIP_LIMIT) {
    return (
      <div className="gap-strip gap-strip--long" aria-hidden="true">
        <span className="gap-strip-worked" />
        <span className="gap-strip-span">at least {gap} shifts</span>
        <span className="gap-strip-worked" />
      </div>
    );
  }
  const cells = Array.from(
    { length: gap + 2 },
    (_, i) => i === 0 || i === gap + 1,
  );
  return (
    <div className="gap-strip" aria-hidden="true">
      {cells.map((worked, i) => (
        <span
          key={i}
          className={worked ? "gap-strip-worked" : "gap-strip-free"}
        />
      ))}
    </div>
  );
}

// The ends a GrowingSlider's scale steps through, each the last doubled,
// stopping at what the kind accepts.
function scaleEnds(min: number, max: number): number[] {
  const ends: number[] = [];
  for (let end = Math.max(min + 1, 8); end < max; end *= 2) ends.push(end);
  ends.push(max);
  return ends;
}

// GrowingSlider is a count across a range that is usually small but need not
// be. Its scale starts short, so small counts are easy to pick out, and grows
// when the thumb reaches its end — then shrinks back once the value is well
// inside the smaller one.
function GrowingSlider({
  label,
  value,
  min,
  max,
  disabled,
  onChange,
}: ControlProps) {
  const ends = scaleEnds(min, max);
  const fitting = ends.find((end) => end >= value) ?? max;
  const [end, setEnd] = useState(fitting);
  const [grew, setGrew] = useState(false);
  const shown = Math.max(end, fitting);

  function move(next: number) {
    const i = ends.indexOf(shown);
    let nextEnd = shown;
    if (next >= shown && i < ends.length - 1) nextEnd = ends[i + 1];
    else if (i > 0 && next <= ends[i - 1] / 2) nextEnd = ends[i - 1];
    if (nextEnd !== shown) {
      setEnd(nextEnd);
      setGrew(nextEnd > shown);
    }
    onChange(next);
  }

  useEffect(() => {
    if (!grew) return;
    const t = window.setTimeout(() => setGrew(false), 450);
    return () => window.clearTimeout(t);
  }, [grew]);

  return (
    <div className="growing-slider">
      <div className="growing-slider-figure">
        <TypedNumber
          label={label}
          value={value}
          min={min}
          max={max}
          disabled={disabled}
          onChange={(n) => {
            setEnd(ends.find((e) => e >= n) ?? max);
            onChange(n);
          }}
          className="rule-figure"
        />
        <span className="rule-stepper-label" aria-hidden="true">
          {label.toLowerCase()}
        </span>
      </div>
      <input
        type="range"
        aria-label={`${label}, slider`}
        min={min}
        max={shown}
        value={value}
        disabled={disabled}
        onChange={(e) => move(Number(e.target.value))}
      />
      <div className="growing-slider-scale" aria-hidden="true">
        <span>{min}</span>
        <span
          className={grew ? "growing-slider-end grew" : "growing-slider-end"}
        >
          {shown}
        </span>
      </div>
    </div>
  );
}

// NumberBox is the fallback for an input this build has no control for.
function NumberBox({
  label,
  value,
  min,
  max,
  disabled,
  onChange,
}: ControlProps) {
  return (
    <label className="settings-field">
      {label}
      <TypedNumber
        label={label}
        value={value}
        min={min}
        max={max}
        disabled={disabled}
        onChange={onChange}
        className="rule-figure"
      />
    </label>
  );
}
