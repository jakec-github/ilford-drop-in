import { useCallback, useEffect, useState } from "react";
import { fetchAvailabilityRound, mintAvailabilityRound } from "../api";
import type { AvailabilityRound } from "../types";

export type MintState = "idle" | "minting" | "error";

interface UseAvailabilityRound {
  // null while the first load is in flight; a round with no entries is a rota
  // nobody has been asked about yet, which the view renders differently.
  round: AvailabilityRound | null;
  error: string | null;
  mintState: MintState;
  mint: () => Promise<void>;
}

// useAvailabilityRound owns the admin's view of the latest rota's round and the
// one action that changes it. They belong together because minting is only worth
// doing to change what the list shows, so the hook adopts the round the mint
// returns rather than making the view remember to reload.
export function useAvailabilityRound(): UseAvailabilityRound {
  const [round, setRound] = useState<AvailabilityRound | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [mintState, setMintState] = useState<MintState>("idle");

  // Written with .then rather than await so no setState is reached
  // synchronously from the effect below.
  const load = useCallback(
    () =>
      fetchAvailabilityRound()
        .then((loaded) => {
          setRound(loaded);
          setError(null);
        })
        .catch((err: unknown) => {
          setError(err instanceof Error ? err.message : "Failed to load");
        }),
    [],
  );

  useEffect(() => {
    void load();
  }, [load]);

  const mint = useCallback(async () => {
    setMintState("minting");
    try {
      setRound(await mintAvailabilityRound());
      setError(null);
      setMintState("idle");
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : "Failed to start the round");
      setMintState("error");
    }
  }, []);

  return { round, error, mintState, mint };
}
