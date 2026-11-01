import { useState } from "react";
import Button from "../ui/Button";
import {
  MAX_DESCRIPTION,
  type PatternEntry,
  type ShiftPatternTranslation,
  type TranslationIssue,
} from "../types";

// How close to the limit a description gets before the counter shows.
const COUNTER_FROM = MAX_DESCRIPTION - 100;

const PLACEHOLDER = "Describe the shift pattern in plain English";

// NaturalPattern is the Shift pattern written in plain English (ADR 0015): one
// field, translated whole by Generate into either the pattern or every issue
// that stops one. It saves nothing itself — a pattern goes up through
// onPattern to the card's own save, as one typed raw would.
//
// descriptions are what the pattern's entries say; with none, there is no
// pattern yet and the field is open. Otherwise they stand in for it, and
// clicking them reopens the field filled with them, one per line.
export default function NaturalPattern({
  descriptions,
  translate,
  onPattern,
  onUseRaw,
}: {
  descriptions: string[];
  translate: (text: string) => Promise<ShiftPatternTranslation>;
  onPattern: (pattern: string, entries: PatternEntry[]) => void;
  // The way out when the model cannot be asked: the raw editor.
  onUseRaw: () => void;
}) {
  const [editing, setEditing] = useState(false);
  const [text, setText] = useState("");
  // What Generate last ran on, so the same text is not sent twice.
  const [tried, setTried] = useState<string | null>(null);
  const [generating, setGenerating] = useState(false);
  const [issues, setIssues] = useState<TranslationIssue[]>([]);
  const [failure, setFailure] = useState<string | null>(null);

  const hasPattern = descriptions.length > 0;
  const open = editing || !hasPattern;
  const length = text.length;
  const canGenerate =
    !generating &&
    text.trim() !== "" &&
    text !== tried &&
    length <= MAX_DESCRIPTION;

  const reset = () => {
    setIssues([]);
    setFailure(null);
    setTried(null);
  };

  const reopen = () => {
    reset();
    setText(descriptions.join("\n"));
    setEditing(true);
  };

  const cancel = () => {
    reset();
    setEditing(false);
  };

  const run = async () => {
    setGenerating(true);
    setTried(text);
    setIssues([]);
    setFailure(null);
    try {
      const result = await translate(text);
      if (result.status === "ok") {
        setEditing(false);
        setTried(null);
        onPattern(result.pattern, result.entries);
      } else {
        setIssues(result.issues);
      }
    } catch (err) {
      setFailure(
        err instanceof Error ? err.message : "Couldn't translate just now",
      );
    } finally {
      setGenerating(false);
    }
  };

  if (!open) {
    return (
      <button
        type="button"
        className="natural-pattern-description"
        title="Describe it again"
        onClick={reopen}
      >
        {descriptions.map((d) => (
          <span key={d}>{d}</span>
        ))}
      </button>
    );
  }

  return (
    <div className="natural-pattern">
      <textarea
        className="natural-pattern-text"
        aria-label="Shift pattern description"
        value={text}
        rows={Math.max(3, text.split("\n").length + 1)}
        placeholder={PLACEHOLDER}
        readOnly={generating}
        onChange={(e) => setText(e.target.value)}
      />
      <div className="natural-pattern-actions">
        <Button size="small" disabled={!canGenerate} onClick={() => void run()}>
          {generating ? "Generating…" : "Generate"}
        </Button>
        {hasPattern && (
          <Button size="small" disabled={generating} onClick={cancel}>
            Cancel
          </Button>
        )}
        {length >= COUNTER_FROM && (
          <span
            className={
              length > MAX_DESCRIPTION
                ? "natural-pattern-count over"
                : "natural-pattern-count"
            }
          >
            {length} / {MAX_DESCRIPTION}
          </span>
        )}
      </div>

      {issues.length > 0 && (
        <ul className="shift-pattern-lines" aria-live="polite">
          {issues.map((issue) => (
            <li key={issue.message} className="problem">
              <span>
                {issue.message}
                {issue.examples.length > 0 && (
                  <span className="natural-pattern-examples">
                    {issue.examples.map((example) => (
                      <code key={example}>{example}</code>
                    ))}
                  </span>
                )}
              </span>
            </li>
          ))}
        </ul>
      )}
      {failure && (
        <p className="settings-error" role="alert">
          {failure}.{" "}
          <button
            type="button"
            className="natural-pattern-raw"
            onClick={onUseRaw}
          >
            Write the entries yourself
          </button>
        </p>
      )}
    </div>
  );
}
