// Split out from setup.ts on purpose: ES module imports are hoisted above
// every other statement in a file, so an import of anything DOM-dependent
// (like @testing-library/react) sitting below this call would still resolve
// before it ran — and libraries like @testing-library/dom bind to `document`
// once, at that first import, permanently. This file has nothing to hoist
// above the registration; bunfig.toml preloads it first, setup.ts second.
import { GlobalRegistrator } from "@happy-dom/global-registrator";

GlobalRegistrator.register();
