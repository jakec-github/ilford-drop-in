import { useRoles } from "./useRoles";
import { patternSet, shapeSet, useRotaDefaults } from "./useRotaDefaults";

// Setup is the least an Organisation must state before a rota can be made:
// Roles, and the Rota card — when shifts run and what each asks for. Each is
// null while it is still being read, so a gate waits rather than flashing the
// "set it up" message at an Organisation that has.
export interface Setup {
  roles: boolean | null;
  rota: boolean | null;
  // Both, or null while either is unknown.
  done: boolean | null;
}

// useSetup says how far an Organisation has got through it. The Rota card is
// Organiser-only, so enabled is false for anyone else: the answer is then null,
// which a caller that is not an Organiser's has no use for anyway.
export function useSetup({
  enabled = true,
}: { enabled?: boolean } = {}): Setup {
  const { roles } = useRoles();
  const { defaults } = useRotaDefaults({ enabled });
  const rolesSet = roles === null ? null : roles.length > 0;
  const rotaSet =
    defaults === null ? null : patternSet(defaults) && shapeSet(defaults);
  const done =
    rolesSet === false || rotaSet === false
      ? false
      : rolesSet === null || rotaSet === null
        ? null
        : true;
  return { roles: rolesSet, rota: rotaSet, done };
}
