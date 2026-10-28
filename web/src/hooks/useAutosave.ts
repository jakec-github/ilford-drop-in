import { useEffect, useRef, useState, type RefObject } from "react";

// How long a control has to stop moving before what it shows is saved: long
// enough that typing a word is one save, short enough that it reads as saving
// itself.
export const SAVE_AFTER_MS = 600;

export type SaveState = "idle" | "saving" | "saved";

// useDraft holds a card's draft of what the server holds. source is what the
// server holds, and toDraft reads a draft from it. It is passed whole rather
// than as a draft so that its identity is what says it moved: a fresh answer
// from the server pulls the card along, unless the card is mid-edit, when what
// is being typed wins and saves over it.
export function useDraft<S, D>({
  source,
  toDraft,
  same,
}: {
  source: S;
  toDraft: (source: S) => D;
  same: (a: D, b: D) => boolean;
}) {
  const [draft, setDraft] = useState<D>(() => toDraft(source));
  // What the server last held, or last accepted from here.
  const saved = useRef<D>(draft);
  const latest = useRef({ toDraft, same });
  useEffect(() => {
    latest.current = { toDraft, same };
  });

  useEffect(() => {
    const stored = latest.current.toDraft(source);
    setDraft((current) =>
      latest.current.same(current, saved.current) ? stored : current,
    );
    saved.current = stored;
  }, [source]);

  return { draft, setDraft, saved };
}

// useSaveWhenSettled saves a draft once it has stopped changing, and says how
// that went. ready says whether it can be sent at all — a rule the server has
// not read yet, or could not. One that is not ready waits, and is sent as soon
// as it is.
export function useSaveWhenSettled<D>({
  draft,
  saved,
  same,
  save,
  ready = true,
}: {
  draft: D;
  saved: RefObject<D>;
  same: (a: D, b: D) => boolean;
  save: (draft: D) => Promise<void>;
  ready?: boolean;
}) {
  const [saveState, setSaveState] = useState<SaveState>("idle");
  const [error, setError] = useState<string | null>(null);
  // The latest of each callback, so a caller passing a new function every
  // render does not restart the wait.
  const latest = useRef({ same, save });
  useEffect(() => {
    latest.current = { same, save };
  });

  useEffect(() => {
    if (!ready || latest.current.same(draft, saved.current)) return;
    const t = window.setTimeout(() => {
      setSaveState("saving");
      setError(null);
      latest.current
        .save(draft)
        .then(() => {
          saved.current = draft;
          setSaveState("saved");
        })
        .catch((err: unknown) => {
          setSaveState("idle");
          // The server's own message names what was wrong. The card keeps
          // what was entered, so it can be put right rather than redone.
          setError(err instanceof Error ? err.message : "It was not saved");
        });
    }, SAVE_AFTER_MS);
    return () => window.clearTimeout(t);
  }, [draft, ready, saved]);

  // "Saved" is said, then fades: a card that saves itself has to show that it
  // did, but it is not news.
  useEffect(() => {
    if (saveState !== "saved") return;
    const t = window.setTimeout(() => setSaveState("idle"), 1600);
    return () => window.clearTimeout(t);
  }, [saveState]);

  return { saveState, error, setError };
}

// useAutosave is a card edited where it stands: both of the above, for a card
// whose draft is ready whenever it differs.
export function useAutosave<S, D>({
  source,
  toDraft,
  same,
  save,
}: {
  source: S;
  toDraft: (source: S) => D;
  same: (a: D, b: D) => boolean;
  save: (draft: D) => Promise<void>;
}) {
  const { draft, setDraft, saved } = useDraft({ source, toDraft, same });
  const status = useSaveWhenSettled({ draft, saved, same, save });
  return { draft, setDraft, ...status };
}
