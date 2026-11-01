import { useState } from "react";
import Button from "../ui/Button";
import { MAX_DESCRIPTION, type TranslationIssue } from "../types";
import "./NaturalField.css";

// How close to the limit a description gets before the counter shows.
const COUNTER_FROM = MAX_DESCRIPTION - 100;

// What a Generate comes to when it does not translate.
interface Issues {
  status: "issues";
  issues: TranslationIssue[];
}

// NaturalField is something written in plain English and translated (ADR
// 0015) — the Shift pattern, or what an Override matches: one field,
// translated whole by Generate into either the thing itself or every issue
// that stops it. It saves nothing itself — what comes back goes up through
// onTranslated to the card's own save, as if it had been typed raw.
//
// descriptions are what is saved, said in words; with none, there is nothing
// yet and the field is open. Otherwise they stand in for it, and clicking them
// reopens the field filled with them, one per line.
export default function NaturalField<T extends { status: "ok" }>({
  descriptions,
  label,
  placeholder,
  translate,
  onTranslated,
  rawLabel,
  onUseRaw,
}: {
  descriptions: string[];
  label: string;
  placeholder: string;
  translate: (text: string) => Promise<T | Issues>;
  onTranslated: (result: T) => void;
  // The way out when the model cannot be asked: the raw editor, and what its
  // button says.
  rawLabel: string;
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
      if (result.status === "issues") {
        setIssues((result as Issues).issues);
      } else {
        setEditing(false);
        setTried(null);
        onTranslated(result as T);
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
        className="natural-field-description"
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
    <div className="natural-field">
      <textarea
        className="natural-field-text"
        aria-label={label}
        value={text}
        rows={Math.max(3, text.split("\n").length + 1)}
        placeholder={placeholder}
        readOnly={generating}
        onChange={(e) => setText(e.target.value)}
      />
      <div className="natural-field-actions">
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
                ? "natural-field-count over"
                : "natural-field-count"
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
                  <span className="natural-field-examples">
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
            className="natural-field-raw"
            onClick={onUseRaw}
          >
            {rawLabel}
          </button>
        </p>
      )}
    </div>
  );
}
